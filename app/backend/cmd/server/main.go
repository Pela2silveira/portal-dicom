package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	_ "github.com/jackc/pgx/v5/stdlib"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type Config struct {
	AppEnv        string
	ListenAddr    string
	PostgresDSN   string
	OrthancURL    string
	OrthancUser   string
	OrthancPass   string
	ConfigPath    string
	MigrationsDir string
	LogLevel      string
}

type App struct {
	cfg            Config
	db             *sql.DB
	httpClient     *http.Client
	logger         *log.Logger
	externalConfig *ExternalConfig
	configLoadedAt time.Time
	identitySource PatientIdentitySource
	professionalIdentitySource ProfessionalIdentitySource
	patientSearchQueue         chan string
	retrieveQueue              chan string
	retrieveEventMu            sync.Mutex
	retrieveEventSubscribers map[string]map[chan RetrieveJobEvent]struct{}
}

type RetrieveJobEvent struct {
	JobID            string `json:"job_id"`
	StudyInstanceUID string `json:"study_instance_uid"`
	Status           string `json:"status"`
	Error            string `json:"error,omitempty"`
}

var ErrPatientIdentityNotFound = errors.New("patient identity not found")
var ErrProfessionalIdentityNotFound = errors.New("professional identity not found")
var ErrProfessionalNotLicensed = errors.New("professional not licensed")

type PatientIdentitySource interface {
	ProviderName() string
	ResolveByDocument(ctx context.Context, documentNumber string) (PatientIdentity, error)
}

type ProfessionalIdentitySource interface {
	ProviderName() string
	ResolveByUsername(ctx context.Context, username string) (ProfessionalIdentity, error)
}

type patientIdentitySourceCloser interface {
	Close(ctx context.Context) error
}

type dependencyHealthReporter interface {
	Healthy() bool
}

type UnavailablePatientIdentitySource struct {
	provider string
	err      error
}

func (s *UnavailablePatientIdentitySource) ProviderName() string {
	if strings.TrimSpace(s.provider) == "" {
		return "unavailable"
	}
	return s.provider
}

func (s *UnavailablePatientIdentitySource) ResolveByDocument(_ context.Context, _ string) (PatientIdentity, error) {
	return PatientIdentity{}, fmt.Errorf("%s unavailable: %w", s.ProviderName(), s.err)
}

func (s *UnavailablePatientIdentitySource) Healthy() bool {
	return false
}

type UnavailableProfessionalIdentitySource struct {
	provider string
	err      error
}

func (s *UnavailableProfessionalIdentitySource) ProviderName() string {
	if strings.TrimSpace(s.provider) == "" {
		return "unavailable"
	}
	return s.provider
}

func (s *UnavailableProfessionalIdentitySource) ResolveByUsername(_ context.Context, _ string) (ProfessionalIdentity, error) {
	return ProfessionalIdentity{}, fmt.Errorf("%s unavailable: %w", s.ProviderName(), s.err)
}

func (s *UnavailableProfessionalIdentitySource) Healthy() bool {
	return false
}

type RetryingPatientIdentitySource struct {
	provider     string
	logger       *log.Logger
	retryEvery   time.Duration
	build        func() (PatientIdentitySource, error)
	current      PatientIdentitySource
	currentMu    sync.RWMutex
	stopCh       chan struct{}
	stopOnce     sync.Once
	refreshMu    sync.Mutex
}

func NewRetryingPatientIdentitySource(provider string, logger *log.Logger, retryEvery time.Duration, build func() (PatientIdentitySource, error)) *RetryingPatientIdentitySource {
	s := &RetryingPatientIdentitySource{
		provider:   provider,
		logger:     logger,
		retryEvery: retryEvery,
		build:      build,
		stopCh:     make(chan struct{}),
	}
	s.current = &UnavailablePatientIdentitySource{
		provider: provider,
		err:      errors.New("provider not initialized"),
	}
	s.refresh()
	go s.retryLoop()
	return s
}

func (s *RetryingPatientIdentitySource) ProviderName() string {
	return s.provider
}

func (s *RetryingPatientIdentitySource) ResolveByDocument(ctx context.Context, documentNumber string) (PatientIdentity, error) {
	s.currentMu.RLock()
	current := s.current
	s.currentMu.RUnlock()
	return current.ResolveByDocument(ctx, documentNumber)
}

func (s *RetryingPatientIdentitySource) Healthy() bool {
	s.currentMu.RLock()
	current := s.current
	s.currentMu.RUnlock()
	if reporter, ok := current.(dependencyHealthReporter); ok {
		return reporter.Healthy()
	}
	return true
}

func (s *RetryingPatientIdentitySource) Close(ctx context.Context) error {
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
	s.currentMu.RLock()
	current := s.current
	s.currentMu.RUnlock()
	if closer, ok := current.(patientIdentitySourceCloser); ok {
		return closer.Close(ctx)
	}
	return nil
}

func (s *RetryingPatientIdentitySource) retryLoop() {
	ticker := time.NewTicker(s.retryEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.refresh()
		case <-s.stopCh:
			return
		}
	}
}

func (s *RetryingPatientIdentitySource) refresh() {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()

	next, err := s.build()
	if err != nil {
		s.currentMu.Lock()
		s.current = &UnavailablePatientIdentitySource{
			provider: s.provider,
			err:      err,
		}
		s.currentMu.Unlock()
		s.logger.Printf(`{"level":"error","msg":"patient_identity_source_retry_failed","provider":%q,"error":%q}`, s.provider, err.Error())
		return
	}

	s.currentMu.Lock()
	prev := s.current
	s.current = next
	s.currentMu.Unlock()
	if closer, ok := prev.(patientIdentitySourceCloser); ok {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = closer.Close(closeCtx)
	}
	s.logger.Printf(`{"level":"info","msg":"patient_identity_source_ready","provider":%q}`, s.provider)
}

type RetryingProfessionalIdentitySource struct {
	provider   string
	logger     *log.Logger
	retryEvery time.Duration
	build      func() (ProfessionalIdentitySource, error)
	current    ProfessionalIdentitySource
	currentMu  sync.RWMutex
	stopCh     chan struct{}
	stopOnce   sync.Once
	refreshMu  sync.Mutex
}

func NewRetryingProfessionalIdentitySource(provider string, logger *log.Logger, retryEvery time.Duration, build func() (ProfessionalIdentitySource, error)) *RetryingProfessionalIdentitySource {
	s := &RetryingProfessionalIdentitySource{
		provider:   provider,
		logger:     logger,
		retryEvery: retryEvery,
		build:      build,
		stopCh:     make(chan struct{}),
	}
	s.current = &UnavailableProfessionalIdentitySource{
		provider: provider,
		err:      errors.New("provider not initialized"),
	}
	s.refresh()
	go s.retryLoop()
	return s
}

func (s *RetryingProfessionalIdentitySource) ProviderName() string {
	return s.provider
}

func (s *RetryingProfessionalIdentitySource) ResolveByUsername(ctx context.Context, username string) (ProfessionalIdentity, error) {
	s.currentMu.RLock()
	current := s.current
	s.currentMu.RUnlock()
	return current.ResolveByUsername(ctx, username)
}

func (s *RetryingProfessionalIdentitySource) Healthy() bool {
	s.currentMu.RLock()
	current := s.current
	s.currentMu.RUnlock()
	if reporter, ok := current.(dependencyHealthReporter); ok {
		return reporter.Healthy()
	}
	return true
}

func (s *RetryingProfessionalIdentitySource) Close(ctx context.Context) error {
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
	s.currentMu.RLock()
	current := s.current
	s.currentMu.RUnlock()
	if closer, ok := current.(patientIdentitySourceCloser); ok {
		return closer.Close(ctx)
	}
	return nil
}

func (s *RetryingProfessionalIdentitySource) retryLoop() {
	ticker := time.NewTicker(s.retryEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.refresh()
		case <-s.stopCh:
			return
		}
	}
}

func (s *RetryingProfessionalIdentitySource) refresh() {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()

	next, err := s.build()
	if err != nil {
		s.currentMu.Lock()
		s.current = &UnavailableProfessionalIdentitySource{
			provider: s.provider,
			err:      err,
		}
		s.currentMu.Unlock()
		s.logger.Printf(`{"level":"error","msg":"professional_identity_source_retry_failed","provider":%q,"error":%q}`, s.provider, err.Error())
		return
	}

	s.currentMu.Lock()
	prev := s.current
	s.current = next
	s.currentMu.Unlock()
	if closer, ok := prev.(patientIdentitySourceCloser); ok {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = closer.Close(closeCtx)
	}
	s.logger.Printf(`{"level":"info","msg":"professional_identity_source_ready","provider":%q}`, s.provider)
}

type PatientIdentity struct {
	DocumentType       string
	DocumentNumber     string
	FullName           string
	BirthDate          string
	Sex                string
	GenderIdentity     string
	Email              string
	AlternateIDs       []PatientAlternateIdentifier
	SourceSystem       string
	LastSynchronizedAt time.Time
}

type ProfessionalIdentity struct {
	Username           string
	DNI                string
	FullName           string
	LicenseNumber      string
	Licensed           bool
	SourceSystem       string
	LastSynchronizedAt time.Time
}

type PatientAlternateIdentifier struct {
	SourceSystem string
	Type         string
	Value        string
	IsPrimary    bool
}

type MongoPacienteDocument struct {
	ID              primitive.ObjectID    `bson:"_id"`
	Documento       any                   `bson:"documento"`
	Nombre          string    `bson:"nombre"`
	Apellido        string    `bson:"apellido"`
	Alias           string    `bson:"alias"`
	Nacionalidad    string    `bson:"nacionalidad"`
	Sexo            string    `bson:"sexo"`
	Genero          string    `bson:"genero"`
	FechaNacimiento time.Time `bson:"fechaNacimiento"`
	Contacto        []MongoPacienteContacto `bson:"contacto"`
}

type MongoProfesionalDocument struct {
	ID                      primitive.ObjectID          `bson:"_id"`
	Documento               any                         `bson:"documento"`
	Nombre                  string                      `bson:"nombre"`
	Apellido                string                      `bson:"apellido"`
	Habilitado              bool                        `bson:"habilitado"`
	ProfesionalMatriculado  bool                        `bson:"profesionalMatriculado"`
	FormacionGrado          []MongoProfesionalFormacion `bson:"formacionGrado"`
}

type MongoProfesionalFormacion struct {
	Matriculacion []MongoProfesionalMatriculacion `bson:"matriculacion"`
}

type MongoProfesionalMatriculacion struct {
	MatriculaNumero any                    `bson:"matriculaNumero"`
	Baja            MongoProfesionalBaja   `bson:"baja"`
}

type MongoProfesionalBaja struct {
	Fecha any `bson:"fecha"`
}

type MongoPacienteContacto struct {
	Activo bool   `bson:"activo"`
	Tipo   string `bson:"tipo"`
	Valor  string `bson:"valor"`
}

type LocalSeedPatientIdentitySource struct{}
type LocalSeedProfessionalIdentitySource struct{}

type MongoPatientIdentitySource struct {
	client         *mongo.Client
	collection     *mongo.Collection
	connectTimeout time.Duration
	queryTimeout   time.Duration
}

type MongoProfessionalIdentitySource struct {
	client         *mongo.Client
	collection     *mongo.Collection
	connectTimeout time.Duration
	queryTimeout   time.Duration
}

func (s *LocalSeedPatientIdentitySource) ProviderName() string {
	return "local_seed"
}

func (s *LocalSeedProfessionalIdentitySource) ProviderName() string {
	return "local_seed"
}

func (s *LocalSeedPatientIdentitySource) ResolveByDocument(_ context.Context, documentNumber string) (PatientIdentity, error) {
	return PatientIdentity{
		DocumentType:   "dni",
		DocumentNumber: documentNumber,
		FullName:       "Paciente " + documentNumber,
		Email:          "paciente." + documentNumber + "@example.invalid",
		SourceSystem:   "landing_mock",
		AlternateIDs: []PatientAlternateIdentifier{
			{
				SourceSystem: "landing_mock",
				Type:         "document_number",
				Value:        documentNumber,
				IsPrimary:    true,
			},
		},
		LastSynchronizedAt: time.Now().UTC(),
	}, nil
}

func (s *LocalSeedProfessionalIdentitySource) ResolveByUsername(_ context.Context, username string) (ProfessionalIdentity, error) {
	dni := digitsOnly(username)
	if dni == "" {
		dni = username
	}
	return ProfessionalIdentity{
		Username:           username,
		DNI:                dni,
		FullName:           "Profesional " + username,
		LicenseNumber:      "MP-" + dni,
		Licensed:           true,
		SourceSystem:       "landing_mock",
		LastSynchronizedAt: time.Now().UTC(),
	}, nil
}

func (s *MongoPatientIdentitySource) ProviderName() string {
	return "his_mongo_direct"
}

func (s *MongoProfessionalIdentitySource) ProviderName() string {
	return "his_mongo_direct"
}

func (s *MongoPatientIdentitySource) ResolveByDocument(ctx context.Context, documentNumber string) (PatientIdentity, error) {
	queryCtx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()

	filter := bson.D{{Key: "$or", Value: mongoDocumentoCandidates(documentNumber)}}
	projection := bson.D{
		{Key: "_id", Value: 1},
		{Key: "documento", Value: 1},
		{Key: "nombre", Value: 1},
		{Key: "apellido", Value: 1},
		{Key: "alias", Value: 1},
		{Key: "nacionalidad", Value: 1},
		{Key: "sexo", Value: 1},
		{Key: "genero", Value: 1},
		{Key: "fechaNacimiento", Value: 1},
		{Key: "contacto", Value: 1},
	}

	var doc MongoPacienteDocument
	err := s.collection.FindOne(queryCtx, filter, options.FindOne().SetProjection(projection)).Decode(&doc)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return PatientIdentity{}, ErrPatientIdentityNotFound
		}
		return PatientIdentity{}, fmt.Errorf("find paciente by documento: %w", err)
	}

	return mongoPacienteToPatientIdentity(documentNumber, doc), nil
}

func (s *MongoPatientIdentitySource) Close(ctx context.Context) error {
	disconnectCtx, cancel := context.WithTimeout(ctx, s.connectTimeout)
	defer cancel()
	return s.client.Disconnect(disconnectCtx)
}

func (s *MongoProfessionalIdentitySource) ResolveByUsername(ctx context.Context, username string) (ProfessionalIdentity, error) {
	documentNumber := digitsOnly(username)
	if documentNumber == "" {
		documentNumber = strings.TrimSpace(username)
	}

	queryCtx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()

	filter := bson.D{{Key: "$or", Value: mongoDocumentoCandidates(documentNumber)}}
	projection := bson.D{
		{Key: "_id", Value: 1},
		{Key: "documento", Value: 1},
		{Key: "nombre", Value: 1},
		{Key: "apellido", Value: 1},
		{Key: "habilitado", Value: 1},
		{Key: "profesionalMatriculado", Value: 1},
		{Key: "formacionGrado.matriculacion.matriculaNumero", Value: 1},
		{Key: "formacionGrado.matriculacion.baja.fecha", Value: 1},
	}

	var doc MongoProfesionalDocument
	err := s.collection.FindOne(queryCtx, filter, options.FindOne().SetProjection(projection)).Decode(&doc)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return ProfessionalIdentity{}, ErrProfessionalIdentityNotFound
		}
		return ProfessionalIdentity{}, fmt.Errorf("find profesional by documento: %w", err)
	}

	resolvedDocument := normalizeMongoDocumento(doc.Documento)
	if resolvedDocument == "" {
		resolvedDocument = documentNumber
	}
	fullName := strings.TrimSpace(strings.TrimSpace(doc.Apellido) + ", " + strings.TrimSpace(doc.Nombre))
	if strings.TrimSpace(doc.Apellido) == "" && strings.TrimSpace(doc.Nombre) != "" {
		fullName = strings.TrimSpace(doc.Nombre)
	}
	if strings.TrimSpace(doc.Apellido) != "" && strings.TrimSpace(doc.Nombre) == "" {
		fullName = strings.TrimSpace(doc.Apellido)
	}
	licenseNumber := activeProfessionalLicenseNumber(doc)
	licensed := doc.Habilitado && doc.ProfesionalMatriculado && strings.TrimSpace(licenseNumber) != ""

	return ProfessionalIdentity{
		Username:           username,
		DNI:                resolvedDocument,
		FullName:           fullName,
		LicenseNumber:      licenseNumber,
		Licensed:           licensed,
		SourceSystem:       "his_mongo_direct",
		LastSynchronizedAt: time.Now().UTC(),
	}, nil
}

func activeProfessionalLicenseNumber(doc MongoProfesionalDocument) string {
	for _, formacion := range doc.FormacionGrado {
		for _, matriculacion := range formacion.Matriculacion {
			if !mongoValueIsNull(matriculacion.Baja.Fecha) {
				continue
			}
			licenseNumber := normalizeMongoDocumento(matriculacion.MatriculaNumero)
			if strings.TrimSpace(licenseNumber) != "" {
				return licenseNumber
			}
		}
	}
	return ""
}

func mongoValueIsNull(value any) bool {
	if value == nil {
		return true
	}
	switch typed := value.(type) {
	case primitive.Null:
		return true
	case primitive.DateTime:
		return false
	case time.Time:
		return typed.IsZero()
	default:
		return false
	}
}

func (s *MongoProfessionalIdentitySource) Close(ctx context.Context) error {
	disconnectCtx, cancel := context.WithTimeout(ctx, s.connectTimeout)
	defer cancel()
	return s.client.Disconnect(disconnectCtx)
}

func mongoPacienteToPatientIdentity(documentNumber string, doc MongoPacienteDocument) PatientIdentity {
	resolvedDocument := normalizeMongoDocumento(doc.Documento)
	if resolvedDocument == "" {
		resolvedDocument = documentNumber
	}

	fullName := strings.TrimSpace(strings.TrimSpace(doc.Apellido) + ", " + strings.TrimSpace(doc.Nombre))
	if strings.TrimSpace(doc.Apellido) == "" && strings.TrimSpace(doc.Nombre) != "" {
		fullName = strings.TrimSpace(doc.Nombre)
	}
	if strings.TrimSpace(doc.Apellido) != "" && strings.TrimSpace(doc.Nombre) == "" {
		fullName = strings.TrimSpace(doc.Apellido)
	}

	identity := PatientIdentity{
		DocumentType:   "dni",
		DocumentNumber: resolvedDocument,
		FullName:       fullName,
		Sex:            strings.TrimSpace(doc.Sexo),
		GenderIdentity: strings.TrimSpace(doc.Genero),
		SourceSystem:   "his_mongo_direct",
		AlternateIDs: []PatientAlternateIdentifier{
			{
				SourceSystem: "his_mongo_direct",
				Type:         "document_number",
				Value:        resolvedDocument,
				IsPrimary:    true,
			},
		},
		LastSynchronizedAt: time.Now().UTC(),
	}

	if !doc.FechaNacimiento.IsZero() {
		identity.BirthDate = doc.FechaNacimiento.UTC().Format("2006-01-02")
	}

	for _, contacto := range doc.Contacto {
		if !contacto.Activo {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(contacto.Tipo), "email") {
			identity.Email = strings.TrimSpace(contacto.Valor)
			if identity.Email != "" {
				break
			}
		}
	}

	if alias := strings.TrimSpace(doc.Alias); alias != "" {
		identity.AlternateIDs = append(identity.AlternateIDs, PatientAlternateIdentifier{
			SourceSystem: "his_mongo_direct",
			Type:         "alias",
			Value:        alias,
			IsPrimary:    false,
		})
	}

	if mongoID := doc.ID.Hex(); mongoID != "" && mongoID != "000000000000000000000000" {
		identity.AlternateIDs = append(identity.AlternateIDs, PatientAlternateIdentifier{
			SourceSystem: "his_mongo_direct",
			Type:         "mongo_object_id",
			Value:        mongoID,
			IsPrimary:    false,
		})
	}

	if identity.Email != "" {
		identity.AlternateIDs = append(identity.AlternateIDs, PatientAlternateIdentifier{
			SourceSystem: "his_mongo_direct",
			Type:         "email",
			Value:        identity.Email,
			IsPrimary:    false,
		})
	}

	return identity
}

type HealthResponse struct {
	Status               string `json:"status"`
	AppEnv               string `json:"app_env"`
	DBOK                 bool   `json:"db_ok"`
	OrthancOK            bool   `json:"orthanc_ok"`
	ConfigOK             bool   `json:"config_ok"`
	IdentityProvidersOK  bool   `json:"identity_providers_ok"`
	CheckedAt            string `json:"checked_at"`
	ConfigPath           string `json:"config_path"`
}

type PatientStudiesResponse struct {
	DocumentNumber string               `json:"document_number"`
	Patient        PatientSummary       `json:"patient"`
	Filters        PatientStudiesFilter `json:"filters"`
	Sync           PatientSyncStatus    `json:"sync"`
	Studies        []PatientStudy       `json:"studies"`
}

type PatientSyncStatus struct {
	RequestID string `json:"request_id,omitempty"`
	Status    string `json:"status"`
	Message   string `json:"message,omitempty"`
}

type PatientSummary struct {
	ID             string `json:"id"`
	DocumentType   string `json:"document_type"`
	DocumentNumber string `json:"document_number"`
	FullName       string `json:"full_name"`
	BirthDate      string `json:"birth_date"`
	Sex            string `json:"sex"`
	GenderIdentity string `json:"gender_identity"`
}

type PatientStudiesFilter struct {
	DateFrom string `json:"date_from,omitempty"`
	DateTo   string `json:"date_to,omitempty"`
	Modality string `json:"modality,omitempty"`
}

type PatientSearchRequest struct {
	DocumentNumber string `json:"document_number"`
	DateFrom       string `json:"date_from,omitempty"`
	DateTo         string `json:"date_to,omitempty"`
	Modality       string `json:"modality,omitempty"`
}

type PatientStudy struct {
	StudyInstanceUID   string   `json:"study_instance_uid"`
	StudyDate          string   `json:"study_date"`
	StudyDescription   string   `json:"study_description"`
	ModalitiesInStudy  []string `json:"modalities_in_study"`
	AvailabilityStatus string   `json:"availability_status"`
	RetrieveStatus     string   `json:"retrieve_status"`
	AuthorizationBasis string   `json:"authorization_basis"`
	ViewerURL          string   `json:"viewer_url,omitempty"`
}

type PatientRetrieveRequest struct {
	DocumentNumber   string `json:"document_number"`
	StudyInstanceUID string `json:"study_instance_uid"`
}

type PatientRetrieveResponse struct {
	JobID            string `json:"job_id"`
	StudyInstanceUID string `json:"study_instance_uid"`
	Status           string `json:"status"`
	ViewerURL        string `json:"viewer_url,omitempty"`
}

type PatientSendCodeRequest struct {
	DocumentNumber string `json:"document_number"`
}

type PatientSendCodeResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

type qidoResponseItem map[string]dicomJSONAttribute

type dicomJSONAttribute struct {
	Value []json.RawMessage `json:"Value"`
}

type PhysicianResultsResponse struct {
	Physician PhysicianSummary       `json:"physician"`
	Filters   PhysicianSearchFilters `json:"filters"`
	Results   []PhysicianResult      `json:"results"`
}

type PhysicianLoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type PhysicianLoginResponse struct {
	Status    string           `json:"status"`
	Message   string           `json:"message"`
	Physician PhysicianSummary `json:"physician,omitempty"`
}

type PhysicianRetrieveRequest struct {
	Username         string `json:"username"`
	StudyInstanceUID string `json:"study_instance_uid"`
}

type PhysicianRetrieveResponse struct {
	JobID            string `json:"job_id"`
	StudyInstanceUID string `json:"study_instance_uid"`
	Status           string `json:"status"`
	ViewerURL        string `json:"viewer_url,omitempty"`
}

type retrieveJobSnapshot struct {
	JobID            string
	StudyInstanceUID string
	Status           string
	Error            string
}

type PhysicianSummary struct {
	ID            string `json:"id"`
	Username      string `json:"username"`
	DNI           string `json:"dni"`
	FullName      string `json:"full_name"`
	LicenseNumber string `json:"license_number"`
}

type PhysicianSearchFilters struct {
	PatientID   string `json:"patient_id,omitempty"`
	PatientName string `json:"patient_name,omitempty"`
	DateFrom    string `json:"date_from,omitempty"`
	DateTo      string `json:"date_to,omitempty"`
	Modality    string `json:"modality,omitempty"`
}

type PhysicianResult struct {
	StudyInstanceUID string   `json:"study_instance_uid"`
	PatientName      string   `json:"patient_name"`
	PatientID        string   `json:"patient_id"`
	StudyDate        string   `json:"study_date"`
	StudyDescription string   `json:"study_description"`
	Modalities       []string `json:"modalities"`
	Locations        []string `json:"locations"`
	CacheStatus      string   `json:"cache_status"`
	RetrieveStatus   string   `json:"retrieve_status"`
	PartialFilter    bool     `json:"partial_filter"`
	ViewerURL        string   `json:"viewer_url,omitempty"`
}

type ConfigResponse struct {
	AppEnv     string             `json:"app_env"`
	ConfigPath string             `json:"config_path"`
	LoadedAt   string             `json:"loaded_at"`
	PACSNodes  []PACSNodeResponse `json:"pacs_nodes"`
	HIS        HISConfigResponse  `json:"his"`
	Patient    PatientConfig      `json:"patient"`
	Professional ProfessionalConfig `json:"professional"`
	Cache      CacheConfig        `json:"cache"`
	Migrations []string           `json:"migrations"`
}

type ExternalConfig struct {
	PACSNodes    []PACSNodeConfig   `json:"pacs_nodes"`
	HIS          HISConfig          `json:"his"`
	Patient      PatientConfig      `json:"patient"`
	Professional ProfessionalConfig `json:"professional"`
	Cache        CacheConfig        `json:"cache"`
}

type PACSNodeConfig struct {
	ID              string         `json:"id"`
	Name            string         `json:"name"`
	Protocol        string         `json:"protocol"`
	Priority        int            `json:"priority"`
	AET             string         `json:"aet"`
	DICOMHost       string         `json:"dicom_host"`
	DICOMPort       int            `json:"dicom_port"`
	DICOMwebBaseURL string         `json:"dicomweb_base_url"`
	SupportsCMove   bool           `json:"supports_cmove"`
	SupportsCGet    bool           `json:"supports_cget"`
	Auth            PACSAuthConfig `json:"auth"`
}

type PACSAuthConfig struct {
	Type            string `json:"type"`
	TokenURL        string `json:"token_url"`
	ClientIDEnv     string `json:"client_id_env"`
	ClientSecretEnv string `json:"client_secret_env"`
}

type HISConfig struct {
	Provider           string `json:"provider"`
	Enabled            bool   `json:"enabled"`
	BaseURL            string `json:"base_url"`
	AuthType           string `json:"auth_type"`
	DocumentLookupPath string `json:"document_lookup_path"`
}

type PatientConfig struct {
	FakeAuth bool `json:"fake_auth"`
}

type ProfessionalConfig struct {
	FakeAuth          bool   `json:"fake_auth"`
	InitialCachePeriod string `json:"initial_cache_period"`
}

type CacheConfig struct {
	OrthancBaseURL string `json:"orthanc_base_url"`
	RetentionDays  int    `json:"retention_days"`
}

type PACSNodeResponse struct {
	ID              string           `json:"id"`
	Name            string           `json:"name"`
	Protocol        string           `json:"protocol"`
	Priority        int              `json:"priority"`
	AET             string           `json:"aet"`
	DICOMHost       string           `json:"dicom_host"`
	DICOMPort       int              `json:"dicom_port"`
	DICOMwebBaseURL string           `json:"dicomweb_base_url"`
	SupportsCMove   bool             `json:"supports_cmove"`
	SupportsCGet    bool             `json:"supports_cget"`
	Auth            PACSAuthResponse `json:"auth"`
}

type PACSAuthResponse struct {
	Type                string `json:"type"`
	TokenURL            string `json:"token_url"`
	ClientIDEnv         string `json:"client_id_env"`
	ClientSecretEnv     string `json:"client_secret_env"`
	ClientIDPresent     bool   `json:"client_id_present"`
	ClientSecretPresent bool   `json:"client_secret_present"`
}

type HISConfigResponse struct {
	Provider           string `json:"provider"`
	Enabled            bool   `json:"enabled"`
	BaseURL            string `json:"base_url"`
	AuthType           string `json:"auth_type"`
	DocumentLookupPath string `json:"document_lookup_path"`
}

func main() {
	cfg := Config{
		AppEnv:        envOrDefault("APP_ENV", "dev"),
		ListenAddr:    envOrDefault("LISTEN_ADDR", ":8081"),
		PostgresDSN:   strings.TrimSpace(os.Getenv("POSTGRES_DSN")),
		OrthancURL:    strings.TrimRight(strings.TrimSpace(os.Getenv("ORTHANC_URL")), "/"),
		OrthancUser:   envOrDefault("ORTHANC_USERNAME", ""),
		OrthancPass:   envOrDefault("ORTHANC_PASSWORD", ""),
		ConfigPath:    envOrDefault("CONFIG_PATH", "/app/config/config.json"),
		MigrationsDir: envOrDefault("MIGRATIONS_DIR", "/app/migrations"),
		LogLevel:      envOrDefault("LOG_LEVEL", "info"),
	}

	logger := log.New(os.Stdout, "", 0)

	var startupIssues []map[string]any

	recordStartupIssue := func(component string, err error) {
		if err == nil {
			return
		}
		startupIssues = append(startupIssues, map[string]any{
			"component": component,
			"error":     err.Error(),
		})
	}

	var db *sql.DB
	var err error
	if cfg.PostgresDSN == "" {
		recordStartupIssue("postgres", errors.New(`missing required env var "POSTGRES_DSN"`))
	} else {
		db, err = sql.Open("pgx", cfg.PostgresDSN)
		if err != nil {
			recordStartupIssue("postgres", fmt.Errorf("open postgres: %w", err))
		}
	}
	if db != nil {
		defer db.Close()
	}

	if cfg.OrthancURL == "" {
		recordStartupIssue("orthanc", errors.New(`missing required env var "ORTHANC_URL"`))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if db != nil {
		if err := db.PingContext(ctx); err != nil {
			recordStartupIssue("postgres", fmt.Errorf("ping postgres: %w", err))
		}
	}

	var appliedMigrations []string
	if db != nil {
		appliedMigrations, err = runMigrations(ctx, db, cfg.MigrationsDir)
		if err != nil {
			recordStartupIssue("migrations", err)
		}
	}

	var externalConfig *ExternalConfig
	externalConfig, err = loadExternalConfig(cfg.ConfigPath)
	if err != nil {
		recordStartupIssue("config", err)
	}

	if externalConfig != nil {
		if err := validateExternalConfig(*externalConfig); err != nil {
			recordStartupIssue("config", err)
			externalConfig = nil
		}
	}

	if db != nil && externalConfig != nil {
		if err := persistExternalConfig(ctx, db, *externalConfig); err != nil {
			recordStartupIssue("config_persist", err)
		}
	}

	identitySource := PatientIdentitySource(&UnavailablePatientIdentitySource{
		provider: "config_unavailable",
		err:      errors.New("external config not loaded"),
	})
	professionalIdentitySource := ProfessionalIdentitySource(&UnavailableProfessionalIdentitySource{
		provider: "config_unavailable",
		err:      errors.New("external config not loaded"),
	})
	if externalConfig != nil {
		identitySource = buildPatientIdentitySource(*externalConfig, logger)
		professionalIdentitySource = buildProfessionalIdentitySource(*externalConfig, logger)
	}

	app := &App{
		cfg: cfg,
		db:  db,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
		logger:         logger,
		externalConfig: externalConfig,
		configLoadedAt: time.Now().UTC(),
		identitySource: identitySource,
		professionalIdentitySource: professionalIdentitySource,
		patientSearchQueue:         make(chan string, 32),
		retrieveQueue:              make(chan string, 32),
		retrieveEventSubscribers: make(map[string]map[chan RetrieveJobEvent]struct{}),
	}

	app.startPatientSearchWorker()
	app.startRetrieveWorker()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/livez", app.handleLivez)
	mux.HandleFunc("/api/health", app.handleHealth)
	mux.HandleFunc("/api/config", app.handleConfig(appliedMigrations))
	mux.HandleFunc("/api/patient/send-code", app.handlePatientSendCode)
	mux.HandleFunc("/api/patient/search", app.handlePatientSearch)
	mux.HandleFunc("/api/patient/studies", app.handlePatientStudies)
	mux.HandleFunc("/api/patient/retrieve", app.handlePatientRetrieve)
	mux.HandleFunc("/api/physician/login", app.handlePhysicianLogin)
	mux.HandleFunc("/api/retrieve/jobs/", app.handleRetrieveJobEvents)
	mux.HandleFunc("/api/physician/results", app.handlePhysicianResults)
	mux.HandleFunc("/api/physician/retrieve", app.handlePhysicianRetrieve)

	if closer, ok := app.identitySource.(patientIdentitySourceCloser); ok {
		defer func() {
			closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := closer.Close(closeCtx); err != nil {
				app.log("error", "patient_identity_source_close_failed", map[string]any{"error": err.Error()})
			}
		}()
	}
	if closer, ok := app.professionalIdentitySource.(patientIdentitySourceCloser); ok {
		defer func() {
			closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := closer.Close(closeCtx); err != nil {
				app.log("error", "professional_identity_source_close_failed", map[string]any{"error": err.Error()})
			}
		}()
	}

	app.log("info", "server_starting", map[string]any{
		"listen_addr":        cfg.ListenAddr,
		"app_env":            cfg.AppEnv,
		"log_level":          cfg.LogLevel,
		"config_path":        cfg.ConfigPath,
		"migrations_dir":     cfg.MigrationsDir,
		"migrations_applied": len(appliedMigrations),
		"pacs_nodes_loaded":  lenPACSNodes(externalConfig),
	})

	for _, issue := range startupIssues {
		app.log("error", "startup_dependency_unavailable", issue)
	}

	app.log("info", "startup_completed", map[string]any{
		"degraded":             len(startupIssues) > 0,
		"startup_issue_count":  len(startupIssues),
	})

	server := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Fatal(err)
	}
}

func (a *App) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	resp := HealthResponse{
		Status:              "ok",
		AppEnv:              a.cfg.AppEnv,
		DBOK:                a.checkDB(ctx),
		OrthancOK:           a.checkOrthanc(ctx),
		ConfigOK:            a.checkConfig(),
		IdentityProvidersOK: a.checkIdentityProviders(),
		CheckedAt:           time.Now().UTC().Format(time.RFC3339),
		ConfigPath:          a.cfg.ConfigPath,
	}

	if !resp.DBOK || !resp.OrthancOK || !resp.ConfigOK || !resp.IdentityProvidersOK {
		resp.Status = "degraded"
	}

	statusCode := http.StatusOK
	if resp.Status != "ok" {
		statusCode = http.StatusServiceUnavailable
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(resp)

	a.log("info", "health_checked", map[string]any{
		"status":      resp.Status,
		"db_ok":       resp.DBOK,
		"orthanc_ok":  resp.OrthancOK,
		"config_ok":   resp.ConfigOK,
		"identity_providers_ok": resp.IdentityProvidersOK,
		"status_code": statusCode,
	})
}

func (a *App) handleLivez(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "alive",
		"ts":     time.Now().UTC().Format(time.RFC3339),
	})
}

func (a *App) handleConfig(appliedMigrations []string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if a.externalConfig == nil {
			http.Error(w, "config not loaded", http.StatusServiceUnavailable)
			return
		}

		resp := ConfigResponse{
			AppEnv:     a.cfg.AppEnv,
			ConfigPath: a.cfg.ConfigPath,
			LoadedAt:   a.configLoadedAt.Format(time.RFC3339),
			HIS: HISConfigResponse{
				Provider:           a.externalConfig.HIS.Provider,
				Enabled:            a.externalConfig.HIS.Enabled,
				BaseURL:            a.externalConfig.HIS.BaseURL,
				AuthType:           a.externalConfig.HIS.AuthType,
				DocumentLookupPath: a.externalConfig.HIS.DocumentLookupPath,
			},
			Patient:      a.externalConfig.Patient,
			Professional: a.externalConfig.Professional,
			Cache:        a.externalConfig.Cache,
			Migrations:   appliedMigrations,
		}

		for _, node := range a.externalConfig.PACSNodes {
			resp.PACSNodes = append(resp.PACSNodes, PACSNodeResponse{
				ID:              node.ID,
				Name:            node.Name,
				Protocol:        node.Protocol,
				Priority:        node.Priority,
				AET:             node.AET,
				DICOMHost:       node.DICOMHost,
				DICOMPort:       node.DICOMPort,
				DICOMwebBaseURL: node.DICOMwebBaseURL,
				SupportsCMove:   node.SupportsCMove,
				SupportsCGet:    node.SupportsCGet,
				Auth: PACSAuthResponse{
					Type:                node.Auth.Type,
					TokenURL:            node.Auth.TokenURL,
					ClientIDEnv:         node.Auth.ClientIDEnv,
					ClientSecretEnv:     node.Auth.ClientSecretEnv,
					ClientIDPresent:     strings.TrimSpace(os.Getenv(node.Auth.ClientIDEnv)) != "",
					ClientSecretPresent: strings.TrimSpace(os.Getenv(node.Auth.ClientSecretEnv)) != "",
				},
			})
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}

func (a *App) startPatientSearchWorker() {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := a.recoverQueuedPatientSearches(ctx); err != nil {
			a.log("error", "patient_search_recovery_failed", map[string]any{"error": err.Error()})
		}
	}()

	go func() {
		for requestID := range a.patientSearchQueue {
			a.processPatientSearchRequest(requestID)
		}
	}()
}

func (a *App) startRetrieveWorker() {
	go func() {
		for jobID := range a.retrieveQueue {
			a.processRetrieveJob(jobID)
		}
	}()
}

func (a *App) recoverQueuedPatientSearches(ctx context.Context) error {
	rows, err := a.db.QueryContext(ctx, `
		SELECT id::text
		FROM search_requests
		WHERE actor_type = 'patient'
		  AND status IN ('queued', 'running')
		ORDER BY created_at ASC
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var requestID string
		if err := rows.Scan(&requestID); err != nil {
			return err
		}
		a.enqueuePatientSearch(requestID)
	}

	return rows.Err()
}

func (a *App) enqueuePatientSearch(requestID string) {
	a.patientSearchQueue <- requestID
}

func (a *App) enqueueRetrieveJob(jobID string) {
	a.retrieveQueue <- jobID
}

func (a *App) handlePatientSendCode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var reqBody PatientSendCodeRequest
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}

	reqBody.DocumentNumber = strings.TrimSpace(reqBody.DocumentNumber)
	if reqBody.DocumentNumber == "" {
		http.Error(w, "document_number is required", http.StatusBadRequest)
		return
	}
	if err := validateDocumentNumber(reqBody.DocumentNumber); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	patient, identity, err := a.ensurePatientRecordWithIdentity(ctx, reqBody.DocumentNumber)
	if err != nil {
		if errors.Is(err, ErrPatientIdentityNotFound) {
			a.log("info", "patient_send_code_patient_not_found", map[string]any{
				"document_number": reqBody.DocumentNumber,
				"provider":        a.identitySource.ProviderName(),
			})
			writeJSON(w, http.StatusNotFound, PatientSendCodeResponse{
				Status:  "patient_not_found",
				Message: "El paciente no cuenta con registros.",
			})
			return
		}

		a.log("error", "patient_send_code_failed", map[string]any{
			"document_number": reqBody.DocumentNumber,
			"provider":        a.identitySource.ProviderName(),
			"error":           err.Error(),
		})
		http.Error(w, "failed to validate patient contact", http.StatusBadGateway)
		return
	}

	if a.externalConfig != nil && a.externalConfig.Patient.FakeAuth {
		a.log("info", "patient_send_code_ready_fake_auth", map[string]any{
			"document_number": reqBody.DocumentNumber,
			"patient_id":      patient.ID,
			"provider":        a.identitySource.ProviderName(),
		})
		writeJSON(w, http.StatusOK, PatientSendCodeResponse{
			Status:  "ready_to_send",
			Message: "Modo demo activo. Se omite la validación real del correo.",
		})
		return
	}

	if strings.TrimSpace(identity.Email) == "" {
		a.log("info", "patient_send_code_missing_email", map[string]any{
			"document_number": reqBody.DocumentNumber,
			"patient_id":      patient.ID,
			"provider":        a.identitySource.ProviderName(),
		})
		writeJSON(w, http.StatusConflict, PatientSendCodeResponse{
			Status:  "missing_active_email",
			Message: "El paciente no tiene mail asociado. Concurra a su centro de salud más cercano para la actualización de sus datos de contacto.",
		})
		return
	}

	a.log("info", "patient_send_code_ready", map[string]any{
		"document_number": reqBody.DocumentNumber,
		"patient_id":      patient.ID,
		"provider":        a.identitySource.ProviderName(),
	})
	writeJSON(w, http.StatusOK, PatientSendCodeResponse{
		Status:  "ready_to_send",
		Message: "Se enviará un código por mail al contacto registrado.",
	})
}

func (a *App) handlePatientSearch(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		a.handlePatientSearchStart(w, r)
	case http.MethodGet:
		a.handlePatientSearchStatus(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *App) handlePatientSearchStart(w http.ResponseWriter, r *http.Request) {
	var reqBody PatientSearchRequest
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}

	reqBody.DocumentNumber = strings.TrimSpace(reqBody.DocumentNumber)
	if reqBody.DocumentNumber == "" {
		http.Error(w, "document_number is required", http.StatusBadRequest)
		return
	}
	if err := validateDocumentNumber(reqBody.DocumentNumber); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	filters := PatientStudiesFilter{
		DateFrom: strings.TrimSpace(reqBody.DateFrom),
		DateTo:   strings.TrimSpace(reqBody.DateTo),
		Modality: strings.ToUpper(strings.TrimSpace(reqBody.Modality)),
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	patient, err := a.ensurePatientRecord(ctx, reqBody.DocumentNumber)
	if err != nil {
		a.log("error", "patient_search_prepare_failed", map[string]any{
			"document_number": reqBody.DocumentNumber,
			"error":           err.Error(),
		})
		http.Error(w, "failed to prepare patient search", http.StatusInternalServerError)
		return
	}

	state, err := a.ensurePatientSearchRequest(ctx, patient, reqBody.DocumentNumber, filters)
	if err != nil {
		a.log("error", "patient_search_enqueue_failed", map[string]any{
			"document_number": reqBody.DocumentNumber,
			"patient_id":      patient.ID,
			"error":           err.Error(),
		})
		http.Error(w, "failed to enqueue patient search", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusAccepted, state)
}

func (a *App) handlePatientSearchStatus(w http.ResponseWriter, r *http.Request) {
	requestID := strings.TrimSpace(r.URL.Query().Get("request_id"))
	if requestID == "" {
		http.Error(w, "request_id is required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	state, err := a.getPatientSearchStateByRequestID(ctx, requestID)
	if err != nil {
		a.log("error", "patient_search_status_failed", map[string]any{
			"request_id": requestID,
			"error":      err.Error(),
		})
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "patient search request not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to load patient search status", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, state)
}

func (a *App) handlePatientStudies(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	documentNumber := strings.TrimSpace(r.URL.Query().Get("document"))
	if documentNumber == "" {
		http.Error(w, "missing required query param: document", http.StatusBadRequest)
		return
	}
	if err := validateDocumentNumber(documentNumber); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	filters := PatientStudiesFilter{
		DateFrom: strings.TrimSpace(r.URL.Query().Get("date_from")),
		DateTo:   strings.TrimSpace(r.URL.Query().Get("date_to")),
		Modality: strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("modality"))),
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	patient, err := a.ensurePatientRecord(ctx, documentNumber)
	if err != nil {
		a.log("error", "patient_seed_failed", map[string]any{
			"document_number": documentNumber,
			"error":           err.Error(),
		})
		http.Error(w, "failed to prepare patient studies", http.StatusInternalServerError)
		return
	}

	syncState, err := a.getPatientSearchState(ctx, patient.ID, filters)
	if err != nil {
		a.log("error", "patient_search_state_failed", map[string]any{
			"document_number": documentNumber,
			"patient_id":      patient.ID,
			"error":           err.Error(),
		})
		http.Error(w, "failed to prepare patient search", http.StatusInternalServerError)
		return
	}

	studies, err := a.listPatientStudies(ctx, patient.ID, filters)
	if err != nil {
		a.log("error", "patient_studies_query_failed", map[string]any{
			"document_number": documentNumber,
			"patient_id":      patient.ID,
			"error":           err.Error(),
		})
		http.Error(w, "failed to query patient studies", http.StatusInternalServerError)
		return
	}

	resp := PatientStudiesResponse{
		DocumentNumber: documentNumber,
		Patient:        patient,
		Filters:        filters,
		Sync:           syncState,
		Studies:        studies,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func patientSearchQueryJSON(documentNumber string, filters PatientStudiesFilter) (string, error) {
	payload, err := json.Marshal(map[string]any{
		"document_number": documentNumber,
		"date_from":       filters.DateFrom,
		"date_to":         filters.DateTo,
		"modality":        filters.Modality,
	})
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

func (a *App) ensurePatientSearchRequest(ctx context.Context, patient PatientSummary, documentNumber string, filters PatientStudiesFilter) (PatientSyncStatus, error) {
	queryJSON, err := patientSearchQueryJSON(documentNumber, filters)
	if err != nil {
		return PatientSyncStatus{}, err
	}

	var existing PatientSyncStatus
	err = a.db.QueryRowContext(ctx, `
		SELECT id::text, status
		FROM search_requests
		WHERE actor_type = 'patient'
		  AND patient_id = $1::uuid
		  AND query_json = $2::jsonb
		  AND status IN ('queued', 'running')
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, patient.ID, queryJSON).Scan(&existing.RequestID, &existing.Status)
	if err == nil {
		existing.Message = patientSyncMessage(existing.Status)
		return existing, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return PatientSyncStatus{}, err
	}

	var requestID string
	err = a.db.QueryRowContext(ctx, `
		INSERT INTO search_requests (
			actor_type, patient_id, query_json, status
		) VALUES (
			'patient', $1::uuid, $2::jsonb, 'queued'
		)
		RETURNING id::text
	`, patient.ID, queryJSON).Scan(&requestID)
	if err != nil {
		return PatientSyncStatus{}, err
	}

	node := a.externalConfig.PACSNodes[0]
	if _, err := a.db.ExecContext(ctx, `
		INSERT INTO search_node_runs (
			search_request_id, node_id, status
		) VALUES (
			$1::uuid, (SELECT id FROM pacs_nodes WHERE code = $2), 'queued'
		)
	`, requestID, node.ID); err != nil {
		return PatientSyncStatus{}, err
	}

	a.enqueuePatientSearch(requestID)

	return PatientSyncStatus{
		RequestID: requestID,
		Status:    "queued",
		Message:   patientSyncMessage("queued"),
	}, nil
}

func (a *App) getPatientSearchState(ctx context.Context, patientID string, filters PatientStudiesFilter) (PatientSyncStatus, error) {
	queryJSON, err := json.Marshal(map[string]any{
		"date_from": filters.DateFrom,
		"date_to":   filters.DateTo,
		"modality":  filters.Modality,
	})
	if err != nil {
		return PatientSyncStatus{}, err
	}

	var state PatientSyncStatus
	err = a.db.QueryRowContext(ctx, `
		SELECT id::text, status
		FROM search_requests
		WHERE actor_type = 'patient'
		  AND patient_id = $1::uuid
		  AND query_json->>'date_from' = ($2::jsonb)->>'date_from'
		  AND query_json->>'date_to' = ($2::jsonb)->>'date_to'
		  AND query_json->>'modality' = ($2::jsonb)->>'modality'
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, patientID, string(queryJSON)).Scan(&state.RequestID, &state.Status)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return PatientSyncStatus{Status: "idle"}, nil
		}
		return PatientSyncStatus{}, err
	}
	state.Message = patientSyncMessage(state.Status)
	return state, nil
}

func (a *App) getPatientSearchStateByRequestID(ctx context.Context, requestID string) (PatientSyncStatus, error) {
	var state PatientSyncStatus
	err := a.db.QueryRowContext(ctx, `
		SELECT id::text, status
		FROM search_requests
		WHERE id = $1::uuid
		  AND actor_type = 'patient'
	`, requestID).Scan(&state.RequestID, &state.Status)
	if err != nil {
		return PatientSyncStatus{}, err
	}
	state.Message = patientSyncMessage(state.Status)
	return state, nil
}

func patientSyncMessage(status string) string {
	switch status {
	case "queued":
		return "Buscando..."
	case "running":
		return "Buscando..."
	case "failed":
		return "No se pudo completar la búsqueda remota."
	default:
		return ""
	}
}

func (a *App) handleRetrieveJobEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	const prefix = "/api/retrieve/jobs/"
	if !strings.HasPrefix(r.URL.Path, prefix) || !strings.HasSuffix(r.URL.Path, "/events") {
		http.NotFound(w, r)
		return
	}

	jobID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, prefix), "/events")
	jobID = strings.Trim(jobID, "/")
	if jobID == "" {
		http.NotFound(w, r)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	initialEvent, err := a.getRetrieveJobEvent(r.Context(), jobID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "failed to load retrieve job", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	if err := writeRetrieveSSEEvent(w, "status", initialEvent); err != nil {
		return
	}
	flusher.Flush()
	if initialEvent.Status == "done" || initialEvent.Status == "failed" {
		return
	}

	subscriber := a.subscribeRetrieveJob(jobID)
	defer a.unsubscribeRetrieveJob(jobID, subscriber)

	for {
		select {
		case <-r.Context().Done():
			return
		case event := <-subscriber:
			if err := writeRetrieveSSEEvent(w, "status", event); err != nil {
				return
			}
			flusher.Flush()
			if event.Status == "done" || event.Status == "failed" {
				return
			}
		}
	}
}

func (a *App) handlePatientRetrieve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var reqBody PatientRetrieveRequest
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}

	reqBody.DocumentNumber = strings.TrimSpace(reqBody.DocumentNumber)
	reqBody.StudyInstanceUID = strings.TrimSpace(reqBody.StudyInstanceUID)
	if reqBody.DocumentNumber == "" || reqBody.StudyInstanceUID == "" {
		http.Error(w, "document_number and study_instance_uid are required", http.StatusBadRequest)
		return
	}
	if err := validateDocumentNumber(reqBody.DocumentNumber); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	patient, err := a.ensurePatientRecord(ctx, reqBody.DocumentNumber)
	if err != nil {
		a.log("error", "patient_retrieve_prepare_failed", map[string]any{
			"document_number": reqBody.DocumentNumber,
			"error":           err.Error(),
		})
		http.Error(w, "failed to prepare patient retrieve", http.StatusInternalServerError)
		return
	}

	resp, err := a.queuePatientRetrieve(ctx, patient, reqBody.StudyInstanceUID)
	if err != nil {
		a.log("error", "patient_retrieve_failed", map[string]any{
			"document_number":   reqBody.DocumentNumber,
			"patient_id":        patient.ID,
			"study_instance_uid": reqBody.StudyInstanceUID,
			"error":             err.Error(),
		})
		http.Error(w, "failed to retrieve patient study", http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (a *App) handlePhysicianResults(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	username := normalizeProfessionalDocumentInput(r.URL.Query().Get("username"))
	if username == "" {
		http.Error(w, "missing required query param: username", http.StatusBadRequest)
		return
	}

	filters := PhysicianSearchFilters{
		PatientID:   strings.TrimSpace(r.URL.Query().Get("patient_id")),
		PatientName: strings.TrimSpace(r.URL.Query().Get("patient_name")),
		DateFrom:    strings.TrimSpace(r.URL.Query().Get("date_from")),
		DateTo:      strings.TrimSpace(r.URL.Query().Get("date_to")),
		Modality:    strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("modality"))),
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	physician, err := a.ensurePhysicianRecord(ctx, username)
	if err != nil {
		if errors.Is(err, ErrProfessionalIdentityNotFound) {
			http.Error(w, "professional not found", http.StatusNotFound)
			return
		}
		if errors.Is(err, ErrProfessionalNotLicensed) {
			http.Error(w, "professional not licensed", http.StatusForbidden)
			return
		}
		a.log("error", "physician_seed_failed", map[string]any{
			"username": username,
			"error":    err.Error(),
		})
		http.Error(w, "failed to prepare physician results", http.StatusInternalServerError)
		return
	}

	results, err := a.listPhysicianResults(ctx, physician.ID, filters)
	if err != nil {
		a.log("error", "physician_results_query_failed", map[string]any{
			"username":     username,
			"physician_id": physician.ID,
			"error":        err.Error(),
		})
		http.Error(w, "failed to query physician results", http.StatusInternalServerError)
		return
	}

	resp := PhysicianResultsResponse{
		Physician: physician,
		Filters:   filters,
		Results:   results,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (a *App) handlePhysicianLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var reqBody PhysicianLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}

	reqBody.Username = normalizeProfessionalDocumentInput(reqBody.Username)
	if reqBody.Username == "" {
		http.Error(w, "username is required", http.StatusBadRequest)
		return
	}
	if a.externalConfig != nil && !a.externalConfig.Professional.FakeAuth {
		writeJSON(w, http.StatusNotImplemented, PhysicianLoginResponse{
			Status:  "provider_not_implemented",
			Message: "La autenticación institucional LDAP/MFA aún no está implementada en este entorno.",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	physician, err := a.ensurePhysicianRecord(ctx, reqBody.Username)
		if err != nil {
			if errors.Is(err, ErrProfessionalIdentityNotFound) {
				writeJSON(w, http.StatusNotFound, PhysicianLoginResponse{
					Status:  "professional_not_found",
					Message: "Profesional no registrado.",
				})
				return
			}
		if errors.Is(err, ErrProfessionalNotLicensed) {
			writeJSON(w, http.StatusForbidden, PhysicianLoginResponse{
				Status:  "professional_not_licensed",
				Message: "El profesional no se encuentra matriculado.",
			})
			return
		}
		http.Error(w, "failed to validate professional access", http.StatusBadGateway)
		return
	}

	writeJSON(w, http.StatusOK, PhysicianLoginResponse{
		Status:    "ready",
		Message:   "Ingreso profesional validado.",
		Physician: physician,
	})
}

func hasPhysicianFilters(filters PhysicianSearchFilters) bool {
	return strings.TrimSpace(filters.PatientID) != "" ||
		strings.TrimSpace(filters.PatientName) != "" ||
		strings.TrimSpace(filters.DateFrom) != "" ||
		strings.TrimSpace(filters.DateTo) != "" ||
		strings.TrimSpace(filters.Modality) != ""
}

func (a *App) handlePhysicianRetrieve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var reqBody PhysicianRetrieveRequest
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}

	reqBody.Username = normalizeProfessionalDocumentInput(reqBody.Username)
	reqBody.StudyInstanceUID = strings.TrimSpace(reqBody.StudyInstanceUID)
	if reqBody.Username == "" || reqBody.StudyInstanceUID == "" {
		http.Error(w, "username and study_instance_uid are required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	physician, err := a.ensurePhysicianRecord(ctx, reqBody.Username)
	if err != nil {
		if errors.Is(err, ErrProfessionalIdentityNotFound) {
			http.Error(w, "professional not found", http.StatusNotFound)
			return
		}
		if errors.Is(err, ErrProfessionalNotLicensed) {
			http.Error(w, "professional not licensed", http.StatusForbidden)
			return
		}
		a.log("error", "physician_retrieve_prepare_failed", map[string]any{
			"username": reqBody.Username,
			"error":    err.Error(),
		})
		http.Error(w, "failed to prepare physician retrieve", http.StatusInternalServerError)
		return
	}

	resp, err := a.queuePhysicianRetrieve(ctx, physician, reqBody.StudyInstanceUID)
	if err != nil {
		a.log("error", "physician_retrieve_failed", map[string]any{
			"username":          reqBody.Username,
			"physician_id":      physician.ID,
			"study_instance_uid": reqBody.StudyInstanceUID,
			"error":             err.Error(),
		})
		http.Error(w, "failed to retrieve physician study", http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (a *App) checkDB(ctx context.Context) bool {
	if a.db == nil {
		a.log("error", "db_unconfigured", map[string]any{})
		return false
	}
	if err := a.db.PingContext(ctx); err != nil {
		a.log("error", "db_ping_failed", map[string]any{"error": err.Error()})
		return false
	}
	return true
}

func (a *App) checkOrthanc(ctx context.Context) bool {
	if strings.TrimSpace(a.cfg.OrthancURL) == "" {
		a.log("error", "orthanc_unconfigured", map[string]any{})
		return false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.cfg.OrthancURL+"/system", nil)
	if err != nil {
		a.log("error", "orthanc_request_build_failed", map[string]any{"error": err.Error()})
		return false
	}
	if a.cfg.OrthancUser != "" {
		req.SetBasicAuth(a.cfg.OrthancUser, a.cfg.OrthancPass)
	}

	res, err := a.httpClient.Do(req)
	if err != nil {
		a.log("error", "orthanc_ping_failed", map[string]any{"error": err.Error()})
		return false
	}
	defer res.Body.Close()

	ok := res.StatusCode >= 200 && res.StatusCode < 300
	if !ok {
		a.log("error", "orthanc_ping_bad_status", map[string]any{
			"status_code": res.StatusCode,
			"url":         a.cfg.OrthancURL + "/system",
		})
	}

	return ok
}

func (a *App) checkIdentityProviders() bool {
	patientOK := true
	if reporter, ok := a.identitySource.(dependencyHealthReporter); ok {
		patientOK = reporter.Healthy()
	}

	professionalOK := true
	if reporter, ok := a.professionalIdentitySource.(dependencyHealthReporter); ok {
		professionalOK = reporter.Healthy()
	}

	if !patientOK {
		a.log("error", "patient_identity_provider_unhealthy", map[string]any{
			"provider": a.identitySource.ProviderName(),
		})
	}
	if !professionalOK {
		a.log("error", "professional_identity_provider_unhealthy", map[string]any{
			"provider": a.professionalIdentitySource.ProviderName(),
		})
	}

	return patientOK && professionalOK
}

func (a *App) checkConfig() bool {
	if a.externalConfig == nil {
		return false
	}

	info, err := os.Stat(a.cfg.ConfigPath)
	if err != nil {
		a.log("error", "config_missing", map[string]any{"error": err.Error()})
		return false
	}

	return !info.IsDir()
}

func loadExternalConfig(path string) (*ExternalConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	cfg := ExternalConfig{
		Patient: PatientConfig{
			FakeAuth: true,
		},
		Professional: ProfessionalConfig{
			FakeAuth:           true,
			InitialCachePeriod: "current_week",
		},
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parse config json: %w", err)
	}

	return &cfg, nil
}

func lenPACSNodes(cfg *ExternalConfig) int {
	if cfg == nil {
		return 0
	}
	return len(cfg.PACSNodes)
}

func buildPatientIdentitySource(cfg ExternalConfig, logger *log.Logger) PatientIdentitySource {
	if !strings.EqualFold(strings.TrimSpace(cfg.HIS.Provider), "his_mongo_direct") {
		return &LocalSeedPatientIdentitySource{}
	}

	return NewRetryingPatientIdentitySource("his_mongo_direct", logger, time.Minute, func() (PatientIdentitySource, error) {
		return connectMongoPatientIdentitySource()
	})
}

func buildProfessionalIdentitySource(cfg ExternalConfig, logger *log.Logger) ProfessionalIdentitySource {
	if !strings.EqualFold(strings.TrimSpace(cfg.HIS.Provider), "his_mongo_direct") {
		return &LocalSeedProfessionalIdentitySource{}
	}

	return NewRetryingProfessionalIdentitySource("his_mongo_direct", logger, time.Minute, func() (ProfessionalIdentitySource, error) {
		return connectMongoProfessionalIdentitySource()
	})
}

func connectMongoPatientIdentitySource() (PatientIdentitySource, error) {
	mongoURI := strings.TrimSpace(os.Getenv("HIS_MONGO_URI"))
	mongoDatabase := strings.TrimSpace(os.Getenv("HIS_MONGO_DATABASE"))
	if mongoURI == "" || mongoDatabase == "" {
		return nil, errors.New("missing required mongo env vars for his_mongo_direct provider")
	}

	connectTimeout := durationFromEnv("HIS_MONGO_CONNECT_TIMEOUT_MS", 5000*time.Millisecond)
	queryTimeout := durationFromEnv("HIS_MONGO_QUERY_TIMEOUT_MS", 3000*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		return nil, fmt.Errorf("connect mongo his provider: %w", err)
	}

	if err := client.Ping(ctx, nil); err != nil {
		_ = client.Disconnect(context.Background())
		return nil, fmt.Errorf("ping mongo his provider: %w", err)
	}

	return &MongoPatientIdentitySource{
		client:         client,
		collection:     client.Database(mongoDatabase).Collection("paciente"),
		connectTimeout: connectTimeout,
		queryTimeout:   queryTimeout,
	}, nil
}

func connectMongoProfessionalIdentitySource() (ProfessionalIdentitySource, error) {
	mongoURI := strings.TrimSpace(os.Getenv("HIS_MONGO_URI"))
	mongoDatabase := strings.TrimSpace(os.Getenv("HIS_MONGO_DATABASE"))
	if mongoURI == "" || mongoDatabase == "" {
		return nil, errors.New("missing required mongo env vars for his_mongo_direct provider")
	}

	connectTimeout := durationFromEnv("HIS_MONGO_CONNECT_TIMEOUT_MS", 5000*time.Millisecond)
	queryTimeout := durationFromEnv("HIS_MONGO_QUERY_TIMEOUT_MS", 3000*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		return nil, fmt.Errorf("connect mongo professional provider: %w", err)
	}

	if err := client.Ping(ctx, nil); err != nil {
		_ = client.Disconnect(context.Background())
		return nil, fmt.Errorf("ping mongo professional provider: %w", err)
	}

	return &MongoProfessionalIdentitySource{
		client:         client,
		collection:     client.Database(mongoDatabase).Collection("profesional"),
		connectTimeout: connectTimeout,
		queryTimeout:   queryTimeout,
	}, nil
}

func nullTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}

func validateDocumentNumber(value string) error {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) < 7 || len(trimmed) > 11 {
		return fmt.Errorf("document must contain between 7 and 11 digits")
	}
	for _, r := range trimmed {
		if !unicode.IsDigit(r) {
			return fmt.Errorf("document must contain digits only")
		}
	}
	return nil
}

func normalizeProfessionalDocumentInput(value string) string {
	return digitsOnly(strings.TrimSpace(value))
}

func buildPatientNameFuzzyQuery(value string) string {
	tokens := tokenizeFuzzySearch(value)
	if len(tokens) == 0 {
		return ""
	}
	return "*" + strings.Join(tokens, "*") + "*"
}

func matchesPatientNameFuzzy(candidate, query string) bool {
	queryTokens := tokenizeFuzzySearch(query)
	if len(queryTokens) == 0 {
		return true
	}
	candidateTokens := tokenizeFuzzySearch(candidate)
	if len(candidateTokens) == 0 {
		return false
	}

	candidateText := strings.Join(candidateTokens, " ")
	for _, token := range queryTokens {
		if !strings.Contains(candidateText, token) {
			return false
		}
	}
	return true
}

func tokenizeFuzzySearch(value string) []string {
	normalized := normalizeFuzzySearchText(value)
	if normalized == "" {
		return nil
	}
	return strings.Fields(normalized)
}

func normalizeFuzzySearchText(value string) string {
	upper := strings.ToUpper(strings.TrimSpace(value))
	if upper == "" {
		return ""
	}

	replacer := strings.NewReplacer(
		"Á", "A",
		"À", "A",
		"Ä", "A",
		"Â", "A",
		"Ã", "A",
		"É", "E",
		"È", "E",
		"Ë", "E",
		"Ê", "E",
		"Í", "I",
		"Ì", "I",
		"Ï", "I",
		"Î", "I",
		"Ó", "O",
		"Ò", "O",
		"Ö", "O",
		"Ô", "O",
		"Õ", "O",
		"Ú", "U",
		"Ù", "U",
		"Ü", "U",
		"Û", "U",
		"Ñ", "N",
	)
	upper = replacer.Replace(upper)

	var b strings.Builder
	b.Grow(len(upper))
	lastWasSpace := true
	for _, r := range upper {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastWasSpace = false
			continue
		}
		if !lastWasSpace {
			b.WriteByte(' ')
			lastWasSpace = true
		}
	}

	return strings.TrimSpace(b.String())
}

func durationFromEnv(name string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(raw + "ms")
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func normalizeMongoDocumento(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case int:
		return fmt.Sprintf("%d", typed)
	case int32:
		return fmt.Sprintf("%d", typed)
	case int64:
		return fmt.Sprintf("%d", typed)
	case float64:
		if typed == float64(int64(typed)) {
			return fmt.Sprintf("%d", int64(typed))
		}
		return strings.TrimSpace(fmt.Sprintf("%.0f", typed))
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func mongoDocumentoCandidates(documentNumber string) bson.A {
	candidates := bson.A{bson.D{{Key: "documento", Value: documentNumber}}}
	if parsed, err := strconv.ParseInt(documentNumber, 10, 64); err == nil {
		if parsed >= math.MinInt32 && parsed <= math.MaxInt32 {
			candidates = append(candidates, bson.D{{Key: "documento", Value: int32(parsed)}})
		}
		candidates = append(candidates,
			bson.D{{Key: "documento", Value: parsed}},
			bson.D{{Key: "documento", Value: float64(parsed)}},
		)
	}
	return candidates
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}

func (a *App) ensurePatientRecordWithIdentity(ctx context.Context, documentNumber string) (PatientSummary, PatientIdentity, error) {
	identity, err := a.identitySource.ResolveByDocument(ctx, documentNumber)
	if err != nil {
		return PatientSummary{}, PatientIdentity{}, err
	}

	patient, err := a.upsertPatientIdentity(ctx, documentNumber, identity)
	if err != nil {
		return PatientSummary{}, PatientIdentity{}, err
	}

	return patient, identity, nil
}

func (a *App) upsertPatientIdentity(ctx context.Context, documentNumber string, identity PatientIdentity) (PatientSummary, error) {
	var patient PatientSummary

	documentType := identity.DocumentType
	if documentType == "" {
		documentType = "dni"
	}
	if strings.TrimSpace(identity.DocumentNumber) == "" {
		identity.DocumentNumber = documentNumber
	}
	if strings.TrimSpace(identity.SourceSystem) == "" {
		identity.SourceSystem = a.identitySource.ProviderName()
	}
	if strings.TrimSpace(identity.FullName) == "" {
		identity.FullName = "Paciente " + identity.DocumentNumber
	}

	err := a.db.QueryRowContext(ctx, `
		INSERT INTO patients (
			document_type, document_number, full_name, birth_date, sex, gender_identity, last_his_sync_at, last_login_at, updated_at
		)
		VALUES ($1, $2, $3, NULLIF($4, '')::date, NULLIF($5, ''), NULLIF($6, ''), $7, now(), now())
		ON CONFLICT (document_type, document_number) DO UPDATE SET
			full_name = EXCLUDED.full_name,
			birth_date = COALESCE(EXCLUDED.birth_date, patients.birth_date),
			sex = COALESCE(EXCLUDED.sex, patients.sex),
			gender_identity = COALESCE(EXCLUDED.gender_identity, patients.gender_identity),
			last_his_sync_at = COALESCE(EXCLUDED.last_his_sync_at, patients.last_his_sync_at),
			last_login_at = now(),
			updated_at = now()
		RETURNING
			id::text,
			document_type,
			document_number,
			COALESCE(full_name, ''),
			COALESCE(to_char(birth_date, 'YYYY-MM-DD'), ''),
			COALESCE(sex, ''),
			COALESCE(gender_identity, '')
	`,
		documentType,
		identity.DocumentNumber,
		identity.FullName,
		identity.BirthDate,
		identity.Sex,
		identity.GenderIdentity,
		nullTime(identity.LastSynchronizedAt),
	).Scan(
		&patient.ID,
		&patient.DocumentType,
		&patient.DocumentNumber,
		&patient.FullName,
		&patient.BirthDate,
		&patient.Sex,
		&patient.GenderIdentity,
	)
	if err != nil {
		return PatientSummary{}, fmt.Errorf("upsert patient: %w", err)
	}

	if len(identity.AlternateIDs) == 0 {
		identity.AlternateIDs = []PatientAlternateIdentifier{
			{
				SourceSystem: identity.SourceSystem,
				Type:         "document_number",
				Value:        documentNumber,
				IsPrimary:    true,
			},
		}
	}

	for _, identifier := range identity.AlternateIDs {
		sourceSystem := strings.TrimSpace(identifier.SourceSystem)
		if sourceSystem == "" {
			sourceSystem = identity.SourceSystem
		}
		if sourceSystem == "" {
			sourceSystem = a.identitySource.ProviderName()
		}

		identifierType := strings.TrimSpace(identifier.Type)
		if identifierType == "" {
			identifierType = "document_number"
		}

		identifierValue := strings.TrimSpace(identifier.Value)
		if identifierValue == "" {
			identifierValue = documentNumber
		}

		if _, err := a.db.ExecContext(ctx, `
			INSERT INTO patient_identifiers (
				patient_id, source_system, identifier_type, identifier_value, is_primary, last_verified_at, metadata_json, updated_at
			) VALUES (
				$1::uuid, $2, $3, $4, $5, now(), '{}'::jsonb, now()
			)
			ON CONFLICT (source_system, identifier_type, identifier_value) DO UPDATE SET
				patient_id = EXCLUDED.patient_id,
				is_primary = EXCLUDED.is_primary,
				last_verified_at = now(),
				updated_at = now()
		`,
			patient.ID,
			sourceSystem,
			identifierType,
			identifierValue,
			identifier.IsPrimary,
		); err != nil {
			return PatientSummary{}, fmt.Errorf("upsert patient identifier: %w", err)
		}
	}

	patient.DocumentType = documentType
	patient.DocumentNumber = identity.DocumentNumber
	patient.FullName = identity.FullName
	if identity.BirthDate != "" {
		patient.BirthDate = identity.BirthDate
	}
	if identity.Sex != "" {
		patient.Sex = identity.Sex
	}
	if identity.GenderIdentity != "" {
		patient.GenderIdentity = identity.GenderIdentity
	}

	return patient, nil
}

func (a *App) ensurePatientRecord(ctx context.Context, documentNumber string) (PatientSummary, error) {
	patient, _, err := a.ensurePatientRecordWithIdentity(ctx, documentNumber)
	if err != nil {
		return PatientSummary{}, fmt.Errorf("resolve patient identity via %s: %w", a.identitySource.ProviderName(), err)
	}
	return patient, nil
}

func (a *App) syncPatientStudiesFromSingleNode(ctx context.Context, patient PatientSummary, documentNumber string, filters PatientStudiesFilter) (PatientSummary, error) {
	if len(a.externalConfig.PACSNodes) != 1 {
		return patient, fmt.Errorf("patient qido flow requires exactly one pacs node, found %d", len(a.externalConfig.PACSNodes))
	}

	node := a.externalConfig.PACSNodes[0]
	if strings.ToLower(node.Protocol) != "qido_rs" {
		return patient, fmt.Errorf("patient qido flow requires qido_rs node, found %s", node.Protocol)
	}

	syncStartedAt := time.Now()
	a.log("info", "patient_qido_sync_started", map[string]any{
		"document_number": documentNumber,
		"patient_id":      patient.ID,
		"node_id":         node.ID,
		"sync_filters":    filters,
	})

	remoteStudies, _, err := a.fetchPatientStudiesFromQIDO(ctx, node, documentNumber, filters)
	if err != nil {
		return patient, err
	}

	if err := a.replacePatientStudyAccessSlice(ctx, patient.ID, node.ID, filters, remoteStudies); err != nil {
		return patient, err
	}

	availableLocalCount := 0
	for _, study := range remoteStudies {
		if study.ViewerURL != "" {
			availableLocalCount++
		}
	}

	a.log("info", "patient_qido_sync_completed", map[string]any{
		"document_number":     documentNumber,
		"patient_id":          patient.ID,
		"node_id":             node.ID,
		"studies_synced":      len(remoteStudies),
		"studies_local_ready": availableLocalCount,
		"duration_ms":         time.Since(syncStartedAt).Milliseconds(),
	})

	return patient, nil
}

func (a *App) processPatientSearchRequest(requestID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	var (
		patientID       string
		documentNumber  string
		status          string
		queryJSONRaw    []byte
	)
	err := a.db.QueryRowContext(ctx, `
		SELECT sr.patient_id::text, p.document_number, sr.status, sr.query_json
		FROM search_requests sr
		JOIN patients p ON p.id = sr.patient_id
		WHERE sr.id = $1::uuid
		  AND sr.actor_type = 'patient'
	`, requestID).Scan(&patientID, &documentNumber, &status, &queryJSONRaw)
	if err != nil {
		a.log("error", "patient_search_load_failed", map[string]any{
			"request_id": requestID,
			"error":      err.Error(),
		})
		return
	}

	if status == "done" {
		return
	}

	var payload struct {
		DateFrom string `json:"date_from"`
		DateTo   string `json:"date_to"`
		Modality string `json:"modality"`
	}
	if len(queryJSONRaw) > 0 {
		if err := json.Unmarshal(queryJSONRaw, &payload); err != nil {
			a.log("error", "patient_search_decode_failed", map[string]any{
				"request_id": requestID,
				"error":      err.Error(),
			})
			return
		}
	}

	if _, err := a.db.ExecContext(ctx, `
		UPDATE search_requests
		SET status = 'running', finished_at = NULL
		WHERE id = $1::uuid
	`, requestID); err != nil {
		a.log("error", "patient_search_mark_running_failed", map[string]any{
			"request_id": requestID,
			"error":      err.Error(),
		})
		return
	}
	if _, err := a.db.ExecContext(ctx, `
		UPDATE search_node_runs
		SET status = 'running', started_at = now(), finished_at = NULL, error = NULL
		WHERE search_request_id = $1::uuid
	`, requestID); err != nil {
		a.log("error", "patient_search_node_running_failed", map[string]any{
			"request_id": requestID,
			"error":      err.Error(),
		})
		return
	}

	patient := PatientSummary{ID: patientID, DocumentNumber: documentNumber}
	filters := PatientStudiesFilter{
		DateFrom: payload.DateFrom,
		DateTo:   payload.DateTo,
		Modality: strings.ToUpper(strings.TrimSpace(payload.Modality)),
	}

	startedAt := time.Now()
	if _, err := a.syncPatientStudiesFromSingleNode(ctx, patient, documentNumber, filters); err != nil {
		_, _ = a.db.ExecContext(ctx, `
			UPDATE search_requests
			SET status = 'failed', finished_at = now()
			WHERE id = $1::uuid
		`, requestID)
		_, _ = a.db.ExecContext(ctx, `
			UPDATE search_node_runs
			SET status = 'failed', finished_at = now(), error = $2
			WHERE search_request_id = $1::uuid
		`, requestID, err.Error())
		a.log("error", "patient_search_failed", map[string]any{
			"request_id": requestID,
			"patient_id": patientID,
			"error":      err.Error(),
		})
		return
	}

	latency := int(time.Since(startedAt).Milliseconds())
	_, _ = a.db.ExecContext(ctx, `
		UPDATE search_requests
		SET status = 'done', finished_at = now()
		WHERE id = $1::uuid
	`, requestID)
	_, _ = a.db.ExecContext(ctx, `
		UPDATE search_node_runs
		SET status = 'done', finished_at = now(), latency_ms = $2, error = NULL
		WHERE search_request_id = $1::uuid
	`, requestID, latency)
}

func (a *App) processRetrieveJob(jobID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	var (
		studyInstanceUID string
		sourceNodeCode   string
		actorType        string
		actorID          string
		status           string
	)
	err := a.db.QueryRowContext(ctx, `
		SELECT
			rj.study_instance_uid,
			COALESCE(pn.code, ''),
			COALESCE(rj.requested_by_actor_type, ''),
			COALESCE(rj.requested_by_actor_id::text, ''),
			rj.status
		FROM retrieve_jobs rj
		LEFT JOIN pacs_nodes pn ON pn.id = rj.source_node_id
		WHERE rj.id = $1::uuid
	`, jobID).Scan(&studyInstanceUID, &sourceNodeCode, &actorType, &actorID, &status)
	if err != nil {
		a.log("error", "retrieve_job_load_failed", map[string]any{
			"job_id": jobID,
			"error":  err.Error(),
		})
		return
	}
	if status == "done" {
		return
	}

	if err := a.updateRetrieveJobStatus(ctx, jobID, "running", "", "", 0, true); err != nil {
		a.log("error", "retrieve_job_mark_running_failed", map[string]any{
			"job_id": jobID,
			"error":  err.Error(),
		})
		return
	}

	node, err := a.getConfiguredNode(sourceNodeCode)
	if err != nil {
		_ = a.updateRetrieveJobStatus(ctx, jobID, "failed", err.Error(), "", 0, false)
		a.log("error", "retrieve_job_node_resolve_failed", map[string]any{
			"job_id":         jobID,
			"source_node_id": sourceNodeCode,
			"error":          err.Error(),
		})
		return
	}

	startedAt := time.Now()
	a.log("info", "retrieve_job_started", map[string]any{
		"job_id":             jobID,
		"study_instance_uid": studyInstanceUID,
		"source_node_id":     sourceNodeCode,
		"actor_type":         actorType,
		"actor_id":           actorID,
	})

	if err := a.ensureOrthancModality(ctx, node); err != nil {
		_ = a.updateRetrieveJobStatus(ctx, jobID, "failed", err.Error(), "", 0, false)
		return
	}

	if err := a.startOrthancCGet(ctx, node, studyInstanceUID); err != nil {
		_ = a.updateRetrieveJobStatus(ctx, jobID, "failed", err.Error(), "", 0, false)
		return
	}

	localReady, orthancStudyID, err := a.waitForStudyInOrthanc(ctx, studyInstanceUID, 2*time.Second, 20*time.Second)
	if err != nil {
		_ = a.updateRetrieveJobStatus(ctx, jobID, "failed", err.Error(), orthancStudyID, 0, false)
		return
	}
	if !localReady {
		err := errors.New("study not available in orthanc after c-get")
		_ = a.updateRetrieveJobStatus(ctx, jobID, "failed", err.Error(), orthancStudyID, 0, false)
		return
	}

	if actorType == "patient" && actorID != "" {
		if err := a.markPatientStudyAvailableLocal(ctx, actorID, studyInstanceUID, orthancStudyID, sourceNodeCode); err != nil {
			_ = a.updateRetrieveJobStatus(ctx, jobID, "failed", err.Error(), orthancStudyID, 0, false)
			return
		}
	}

	if err := a.upsertCachedStudy(ctx, studyInstanceUID, orthancStudyID, []string{sourceNodeCode}, "local_complete"); err != nil {
		_ = a.updateRetrieveJobStatus(ctx, jobID, "failed", err.Error(), orthancStudyID, 0, false)
		return
	}

	if err := a.updateRetrieveJobStatus(ctx, jobID, "done", "", orthancStudyID, 0, false); err != nil {
		a.log("error", "retrieve_job_mark_done_failed", map[string]any{
			"job_id": jobID,
			"error":  err.Error(),
		})
		return
	}

	a.log("info", "retrieve_job_completed", map[string]any{
		"job_id":            jobID,
		"study_instance_uid": studyInstanceUID,
		"source_node_id":    sourceNodeCode,
		"actor_type":        actorType,
		"actor_id":          actorID,
		"orthanc_study_id":  orthancStudyID,
		"duration_ms":       time.Since(startedAt).Milliseconds(),
	})
}

func (a *App) replacePatientStudyAccessSlice(ctx context.Context, patientID, sourceNodeID string, filters PatientStudiesFilter, studies []PatientStudy) error {
	if err := a.deletePatientStudyAccessSlice(ctx, patientID, filters); err != nil {
		return fmt.Errorf("clear patient study access slice: %w", err)
	}

	for _, study := range studies {
		sourceJSON, err := json.Marshal(map[string]any{
			"study_date":          study.StudyDate,
			"study_description":   study.StudyDescription,
			"modalities_in_study": study.ModalitiesInStudy,
			"source_node_id":      sourceNodeID,
		})
		if err != nil {
			return fmt.Errorf("marshal patient qido study: %w", err)
		}

		availabilityStatus := "pending_retrieve"
		if study.ViewerURL != "" {
			availabilityStatus = "available_local"
		}

		if _, err := a.db.ExecContext(ctx, `
			INSERT INTO patient_study_access (
				patient_id, study_instance_uid, authorization_basis, availability_status,
				local_orthanc_study_id, first_seen_at, last_seen_at, last_authorized_at, source_json
			) VALUES (
				$1::uuid, $2, 'patient_document_qido_match', $3, NULL, now(), now(), now(), $4::jsonb
			)
			ON CONFLICT (patient_id, study_instance_uid) DO UPDATE SET
				authorization_basis = EXCLUDED.authorization_basis,
				availability_status = EXCLUDED.availability_status,
				last_seen_at = now(),
				last_authorized_at = now(),
				source_json = EXCLUDED.source_json
		`,
			patientID,
			study.StudyInstanceUID,
			availabilityStatus,
			string(sourceJSON),
		); err != nil {
			return fmt.Errorf("insert qido-backed patient study access: %w", err)
		}
	}

	return nil
}

func (a *App) deletePatientStudyAccessSlice(ctx context.Context, patientID string, filters PatientStudiesFilter) error {
	query := `
		DELETE FROM patient_study_access
		WHERE patient_id = $1::uuid
	`
	args := []any{patientID}
	position := 2

	if filters.DateFrom != "" {
		query += fmt.Sprintf(` AND REPLACE(COALESCE(source_json->>'study_date', ''), '-', '') >= REPLACE($%d, '-', '')`, position)
		args = append(args, filters.DateFrom)
		position++
	}
	if filters.DateTo != "" {
		query += fmt.Sprintf(` AND REPLACE(COALESCE(source_json->>'study_date', ''), '-', '') <= REPLACE($%d, '-', '')`, position)
		args = append(args, filters.DateTo)
		position++
	}
	if filters.Modality != "" {
		query += fmt.Sprintf(` AND EXISTS (
			SELECT 1
			FROM jsonb_array_elements_text(COALESCE(source_json->'modalities_in_study', '[]'::jsonb)) AS modality
			WHERE UPPER(modality) = $%d
		)`, position)
		args = append(args, filters.Modality)
		position++
	}

	_, err := a.db.ExecContext(ctx, query, args...)
	return err
}

func (a *App) queuePatientRetrieve(ctx context.Context, patient PatientSummary, studyInstanceUID string) (PatientRetrieveResponse, error) {
	activeJob, err := a.findActiveRetrieveJob(ctx, studyInstanceUID, "patient", patient.ID)
	if err != nil {
		return PatientRetrieveResponse{}, err
	}
	if activeJob != nil {
		return PatientRetrieveResponse{
			JobID:            activeJob.JobID,
			StudyInstanceUID: activeJob.StudyInstanceUID,
			Status:           activeJob.Status,
		}, nil
	}

	_, sourceNodeID, err := a.getPatientSourceNode(ctx, patient.ID, studyInstanceUID)
	if err != nil {
		return PatientRetrieveResponse{}, err
	}

	jobID, err := a.insertRetrieveJob(ctx, studyInstanceUID, sourceNodeID, "patient", patient.ID)
	if err != nil {
		return PatientRetrieveResponse{}, fmt.Errorf("insert retrieve job: %w", err)
	}
	a.log("info", "patient_retrieve_queued", map[string]any{
		"patient_id":         patient.ID,
		"study_instance_uid": studyInstanceUID,
		"source_node_id":     sourceNodeID,
		"job_id":             jobID,
	})
	a.enqueueRetrieveJob(jobID)

	return PatientRetrieveResponse{
		JobID:            jobID,
		StudyInstanceUID: studyInstanceUID,
		Status:           "queued",
	}, nil
}

func (a *App) queuePhysicianRetrieve(ctx context.Context, physician PhysicianSummary, studyInstanceUID string) (PhysicianRetrieveResponse, error) {
	activeJob, err := a.findActiveRetrieveJob(ctx, studyInstanceUID, "physician", physician.ID)
	if err != nil {
		return PhysicianRetrieveResponse{}, err
	}
	if activeJob != nil {
		return PhysicianRetrieveResponse{
			JobID:            activeJob.JobID,
			StudyInstanceUID: activeJob.StudyInstanceUID,
			Status:           activeJob.Status,
		}, nil
	}

	_, sourceNodeID, err := a.getPhysicianSourceNode(ctx, studyInstanceUID)
	if err != nil {
		return PhysicianRetrieveResponse{}, err
	}

	jobID, err := a.insertRetrieveJob(ctx, studyInstanceUID, sourceNodeID, "physician", physician.ID)
	if err != nil {
		return PhysicianRetrieveResponse{}, fmt.Errorf("insert retrieve job: %w", err)
	}
	a.log("info", "physician_retrieve_queued", map[string]any{
		"physician_id":       physician.ID,
		"study_instance_uid": studyInstanceUID,
		"source_node_id":     sourceNodeID,
		"job_id":             jobID,
	})
	a.enqueueRetrieveJob(jobID)

	return PhysicianRetrieveResponse{
		JobID:            jobID,
		StudyInstanceUID: studyInstanceUID,
		Status:           "queued",
	}, nil
}

func (a *App) findActiveRetrieveJob(ctx context.Context, studyUID, actorType, actorID string) (*retrieveJobSnapshot, error) {
	var snapshot retrieveJobSnapshot
	err := a.db.QueryRowContext(ctx, `
		SELECT id::text, study_instance_uid, status, COALESCE(error, '')
		FROM retrieve_jobs
		WHERE study_instance_uid = $1
		  AND requested_by_actor_type = $2
		  AND requested_by_actor_id = $3::uuid
		  AND status IN ('queued', 'running')
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, studyUID, actorType, actorID).Scan(&snapshot.JobID, &snapshot.StudyInstanceUID, &snapshot.Status, &snapshot.Error)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find active retrieve job: %w", err)
	}
	return &snapshot, nil
}

func (a *App) getRetrieveJobEvent(ctx context.Context, jobID string) (RetrieveJobEvent, error) {
	var event RetrieveJobEvent
	err := a.db.QueryRowContext(ctx, `
		SELECT id::text, study_instance_uid, status, COALESCE(error, '')
		FROM retrieve_jobs
		WHERE id = $1::uuid
	`, jobID).Scan(&event.JobID, &event.StudyInstanceUID, &event.Status, &event.Error)
	return event, err
}

func (a *App) subscribeRetrieveJob(jobID string) chan RetrieveJobEvent {
	a.retrieveEventMu.Lock()
	defer a.retrieveEventMu.Unlock()

	ch := make(chan RetrieveJobEvent, 4)
	if a.retrieveEventSubscribers[jobID] == nil {
		a.retrieveEventSubscribers[jobID] = make(map[chan RetrieveJobEvent]struct{})
	}
	a.retrieveEventSubscribers[jobID][ch] = struct{}{}
	return ch
}

func (a *App) unsubscribeRetrieveJob(jobID string, ch chan RetrieveJobEvent) {
	a.retrieveEventMu.Lock()
	defer a.retrieveEventMu.Unlock()

	subscribers := a.retrieveEventSubscribers[jobID]
	if subscribers == nil {
		close(ch)
		return
	}
	delete(subscribers, ch)
	if len(subscribers) == 0 {
		delete(a.retrieveEventSubscribers, jobID)
	}
	close(ch)
}

func (a *App) publishRetrieveJobEvent(event RetrieveJobEvent) {
	a.retrieveEventMu.Lock()
	defer a.retrieveEventMu.Unlock()

	for subscriber := range a.retrieveEventSubscribers[event.JobID] {
		select {
		case subscriber <- event:
		default:
		}
	}
}

func writeRetrieveSSEEvent(w io.Writer, eventName string, event RetrieveJobEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\n", eventName); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
		return err
	}
	return nil
}

func (a *App) fetchPatientStudiesFromQIDO(ctx context.Context, node PACSNodeConfig, documentNumber string, filters PatientStudiesFilter) ([]PatientStudy, string, error) {
	qidoStartedAt := time.Now()
	token, err := a.fetchPACSBearerToken(ctx, node)
	if err != nil {
		return nil, "", fmt.Errorf("fetch pacs token for %s: %w", node.ID, err)
	}

	endpoint, err := url.Parse(strings.TrimRight(node.DICOMwebBaseURL, "/") + "/studies")
	if err != nil {
		return nil, "", fmt.Errorf("build qido url: %w", err)
	}

	query := endpoint.Query()
	query.Set("PatientID", documentNumber)
	if filters.DateFrom != "" || filters.DateTo != "" {
		query.Set("StudyDate", buildQIDODateRange(filters.DateFrom, filters.DateTo))
	}
	query.Set("limit", "50")
	query.Add("includefield", "StudyInstanceUID")
	query.Add("includefield", "StudyDate")
	query.Add("includefield", "StudyDescription")
	query.Add("includefield", "ModalitiesInStudy")
	query.Add("includefield", "PatientName")
	endpoint.RawQuery = query.Encode()

	a.log("info", "patient_qido_request_started", map[string]any{
		"document_number": documentNumber,
		"node_id":         node.ID,
		"url":             endpoint.String(),
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, "", fmt.Errorf("build qido request: %w", err)
	}
	req.Header.Set("Accept", "application/dicom+json, application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	res, err := a.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("execute qido request: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return nil, "", fmt.Errorf("qido bad status %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload []qidoResponseItem
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		if errors.Is(err, io.EOF) {
			payload = []qidoResponseItem{}
		} else {
		return nil, "", fmt.Errorf("decode qido response: %w", err)
		}
	}

	studies := make([]PatientStudy, 0, len(payload))
	patientName := ""
	for _, item := range payload {
		studyUID := dicomFirstString(item, "0020000D")
		if studyUID == "" {
			continue
		}

		study := PatientStudy{
			StudyInstanceUID:   studyUID,
			StudyDate:          normalizeStudyDate(dicomFirstString(item, "00080020")),
			StudyDescription:   dicomFirstString(item, "00081030"),
			ModalitiesInStudy:  dicomStringList(item, "00080061"),
			AvailabilityStatus: "pending_retrieve",
			AuthorizationBasis: "patient_document_qido_match",
		}

		cached, err := a.isStudyAvailableLocal(ctx, studyUID)
		if err != nil {
			return nil, "", fmt.Errorf("check local cache for study %s: %w", studyUID, err)
		}
		if cached {
			study.AvailabilityStatus = "available_local"
			study.ViewerURL = buildOHIFViewerURL(studyUID)
		}

		if patientName == "" {
			patientName = dicomFirstPersonName(item, "00100010")
		}

		studies = append(studies, study)
	}

	sort.Slice(studies, func(i, j int) bool {
		if studies[i].StudyDate == studies[j].StudyDate {
			return studies[i].StudyInstanceUID < studies[j].StudyInstanceUID
		}
		return studies[i].StudyDate > studies[j].StudyDate
	})

	a.log("info", "patient_qido_request_completed", map[string]any{
		"document_number": documentNumber,
		"node_id":         node.ID,
		"study_count":     len(studies),
		"duration_ms":     time.Since(qidoStartedAt).Milliseconds(),
	})

	return studies, patientName, nil
}

func (a *App) fetchPACSBearerToken(ctx context.Context, node PACSNodeConfig) (string, error) {
	if node.Auth.Type == "" {
		return "", nil
	}
	if node.Auth.Type != "keycloak_client_credentials" {
		return "", fmt.Errorf("unsupported pacs auth type %q", node.Auth.Type)
	}

	clientID := strings.TrimSpace(os.Getenv(node.Auth.ClientIDEnv))
	clientSecret := strings.TrimSpace(os.Getenv(node.Auth.ClientSecretEnv))
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)

	tokenStartedAt := time.Now()
	tokenURL, err := url.Parse(node.Auth.TokenURL)
	if err != nil {
		return "", fmt.Errorf("parse token url: %w", err)
	}
	a.log("info", "pacs_token_request_started", map[string]any{
		"node_id":     node.ID,
		"auth_type":   node.Auth.Type,
		"token_host":  tokenURL.Host,
		"token_path":  tokenURL.Path,
		"client_id_env": node.Auth.ClientIDEnv,
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, node.Auth.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	res, err := a.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("execute token request: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return "", fmt.Errorf("token bad status %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("decode token response: %w", err)
	}
	if strings.TrimSpace(payload.AccessToken) == "" {
		return "", errors.New("empty access_token in token response")
	}

	a.log("info", "pacs_token_request_completed", map[string]any{
		"node_id":      node.ID,
		"auth_type":    node.Auth.Type,
		"token_host":   tokenURL.Host,
		"duration_ms":  time.Since(tokenStartedAt).Milliseconds(),
	})

	return payload.AccessToken, nil
}

func (a *App) isStudyAvailableLocal(ctx context.Context, studyUID string) (bool, error) {
	ok, _, err := a.findOrthancStudy(ctx, studyUID)
	return ok, err
}

func (a *App) findOrthancStudy(ctx context.Context, studyUID string) (bool, string, error) {
	endpoint := strings.TrimRight(a.cfg.OrthancURL, "/") + "/dicom-web/studies?StudyInstanceUID=" + url.QueryEscape(studyUID) + "&limit=1"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false, "", fmt.Errorf("build orthanc qido request: %w", err)
	}
	req.Header.Set("Accept", "application/dicom+json, application/json")
	if a.cfg.OrthancUser != "" {
		req.SetBasicAuth(a.cfg.OrthancUser, a.cfg.OrthancPass)
	}

	res, err := a.httpClient.Do(req)
	if err != nil {
		return false, "", fmt.Errorf("execute orthanc qido request: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return false, "", fmt.Errorf("orthanc qido bad status %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload []qidoResponseItem
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return false, "", fmt.Errorf("decode orthanc qido response: %w", err)
	}

	if len(payload) == 0 {
		return false, "", nil
	}

	lookupReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(a.cfg.OrthancURL, "/")+"/tools/find", strings.NewReader(`{"Level":"Study","Query":{"StudyInstanceUID":"`+studyUID+`"}}`))
	if err != nil {
		return true, "", nil
	}
	lookupReq.Header.Set("Content-Type", "application/json")
	if a.cfg.OrthancUser != "" {
		lookupReq.SetBasicAuth(a.cfg.OrthancUser, a.cfg.OrthancPass)
	}
	lookupRes, err := a.httpClient.Do(lookupReq)
	if err != nil {
		return true, "", nil
	}
	defer lookupRes.Body.Close()
	if lookupRes.StatusCode < 200 || lookupRes.StatusCode >= 300 {
		return true, "", nil
	}
	var ids []string
	if err := json.NewDecoder(lookupRes.Body).Decode(&ids); err != nil || len(ids) == 0 {
		return true, "", nil
	}

	return true, ids[0], nil
}

func (a *App) getPatientSourceNode(ctx context.Context, patientID, studyInstanceUID string) (PACSNodeConfig, string, error) {
	var sourceJSONRaw []byte
	if err := a.db.QueryRowContext(ctx, `
		SELECT source_json
		FROM patient_study_access
		WHERE patient_id = $1::uuid
		  AND study_instance_uid = $2
	`, patientID, studyInstanceUID).Scan(&sourceJSONRaw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return PACSNodeConfig{}, "", errors.New("patient study not found")
		}
		return PACSNodeConfig{}, "", err
	}

	var source struct {
		SourceNodeID string `json:"source_node_id"`
	}
	_ = json.Unmarshal(sourceJSONRaw, &source)
	if source.SourceNodeID == "" {
		if len(a.externalConfig.PACSNodes) == 1 {
			return a.externalConfig.PACSNodes[0], a.externalConfig.PACSNodes[0].ID, nil
		}
		return PACSNodeConfig{}, "", errors.New("source node id missing for patient study")
	}

	for _, node := range a.externalConfig.PACSNodes {
		if node.ID == source.SourceNodeID {
			return node, source.SourceNodeID, nil
		}
	}

	return PACSNodeConfig{}, "", fmt.Errorf("unknown source node id %q", source.SourceNodeID)
}

func (a *App) getPhysicianSourceNode(ctx context.Context, studyInstanceUID string) (PACSNodeConfig, string, error) {
	var locationsJSONRaw []byte
	err := a.db.QueryRowContext(ctx, `
		SELECT locations_json
		FROM cached_studies
		WHERE study_instance_uid = $1
	`, studyInstanceUID).Scan(&locationsJSONRaw)
	if err == nil {
		var locations []string
		if len(locationsJSONRaw) > 0 && json.Unmarshal(locationsJSONRaw, &locations) == nil {
			for _, location := range locations {
				for _, node := range a.externalConfig.PACSNodes {
					if node.ID == location || strings.EqualFold(node.Name, location) || strings.EqualFold(node.ID, location) {
						return node, node.ID, nil
					}
				}
			}
		}
	}
	if len(a.externalConfig.PACSNodes) == 1 {
		return a.externalConfig.PACSNodes[0], a.externalConfig.PACSNodes[0].ID, nil
	}
	return PACSNodeConfig{}, "", fmt.Errorf("source node not resolved for physician study %q", studyInstanceUID)
}

func (a *App) getConfiguredNode(nodeID string) (PACSNodeConfig, error) {
	for _, node := range a.externalConfig.PACSNodes {
		if node.ID == nodeID {
			return node, nil
		}
	}
	return PACSNodeConfig{}, fmt.Errorf("configured PACS node %q not found", nodeID)
}

func (a *App) getStudyOperationalState(ctx context.Context, studyUID string, fallbackCacheStatus, fallbackRetrieveStatus string) (string, string, string, error) {
	cacheStatus := fallbackCacheStatus
	retrieveStatus := fallbackRetrieveStatus
	viewerURL := ""

	var cachedOrthancStudyID string
	err := a.db.QueryRowContext(ctx, `
		SELECT COALESCE(cache_status, ''), COALESCE(orthanc_study_id, '')
		FROM cached_studies
		WHERE study_instance_uid = $1
	`, studyUID).Scan(&cacheStatus, &cachedOrthancStudyID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", "", "", err
	}
	if cacheStatus == "" {
		cacheStatus = fallbackCacheStatus
	}

	var (
		latestRetrieveStatus string
		retrieveCreatedAt    time.Time
		retrieveStartedAt    sql.NullTime
		retrieveFinishedAt   sql.NullTime
	)
	err = a.db.QueryRowContext(ctx, `
		SELECT status, created_at, started_at, finished_at
		FROM retrieve_jobs
		WHERE study_instance_uid = $1
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, studyUID).Scan(&latestRetrieveStatus, &retrieveCreatedAt, &retrieveStartedAt, &retrieveFinishedAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", "", "", err
	}
	if latestRetrieveStatus != "" {
		retrieveStatus = latestRetrieveStatus
		if (latestRetrieveStatus == "queued" || latestRetrieveStatus == "running") && !retrieveFinishedAt.Valid {
			lastActivity := retrieveCreatedAt
			if retrieveStartedAt.Valid {
				lastActivity = retrieveStartedAt.Time
			}
			if time.Since(lastActivity) > 10*time.Minute {
				retrieveStatus = "idle"
			}
		}
	}

	isLocal, _, err := a.findOrthancStudy(ctx, studyUID)
	if err != nil {
		return "", "", "", err
	}
	if isLocal {
		cacheStatus = "local_complete"
		retrieveStatus = "done"
		viewerURL = buildOHIFViewerURL(studyUID)
	}

	return cacheStatus, retrieveStatus, viewerURL, nil
}

func (a *App) insertRetrieveJob(ctx context.Context, studyUID, sourceNodeID, actorType, actorID string) (string, error) {
	var jobID string
	err := a.db.QueryRowContext(ctx, `
		INSERT INTO retrieve_jobs (
			study_instance_uid, source_node_id, requested_by_actor_type, requested_by_actor_id, status
		) VALUES (
			$1, (SELECT id FROM pacs_nodes WHERE code = $2), $3, $4::uuid, 'queued'
		)
		RETURNING id::text
	`, studyUID, sourceNodeID, actorType, actorID).Scan(&jobID)
	return jobID, err
}

func (a *App) updateRetrieveJobStatus(ctx context.Context, jobID, status, errMsg, orthancStudyID string, instancesReceived int, setStarted bool) error {
	query := `
		UPDATE retrieve_jobs
		SET status = $2,
		    error = NULLIF($3, ''),
		    orthanc_study_id = NULLIF($4, ''),
		    instances_received = NULLIF($5, 0),
		    finished_at = CASE WHEN $2 IN ('done', 'failed') THEN now() ELSE finished_at END
	`
	args := []any{jobID, status, errMsg, orthancStudyID, instancesReceived}
	if setStarted {
		query += `, started_at = now()`
	}
	query += ` WHERE id = $1::uuid`
	if _, err := a.db.ExecContext(ctx, query, args...); err != nil {
		return err
	}

	event, err := a.getRetrieveJobEvent(ctx, jobID)
	if err == nil {
		a.publishRetrieveJobEvent(event)
	}
	return nil
}

func (a *App) ensureOrthancModality(ctx context.Context, node PACSNodeConfig) error {
	payload, err := json.Marshal(map[string]any{
		"AET":            node.AET,
		"Host":           node.DICOMHost,
		"Port":           node.DICOMPort,
		"RetrieveMethod": "C-GET",
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, strings.TrimRight(a.cfg.OrthancURL, "/")+"/modalities/"+url.PathEscape(node.ID), strings.NewReader(string(payload)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if a.cfg.OrthancUser != "" {
		req.SetBasicAuth(a.cfg.OrthancUser, a.cfg.OrthancPass)
	}

	res, err := a.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return fmt.Errorf("orthanc modality put bad status %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func (a *App) startOrthancCGet(ctx context.Context, node PACSNodeConfig, studyInstanceUID string) error {
	payload, err := json.Marshal(map[string]any{
		"Level": "Study",
		"Resources": []map[string]string{
			{"StudyInstanceUID": studyInstanceUID},
		},
		"Timeout": 60,
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(a.cfg.OrthancURL, "/")+"/modalities/"+url.PathEscape(node.ID)+"/get", strings.NewReader(string(payload)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if a.cfg.OrthancUser != "" {
		req.SetBasicAuth(a.cfg.OrthancUser, a.cfg.OrthancPass)
	}

	orthancRetrieveClient := &http.Client{}
	res, err := orthancRetrieveClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return fmt.Errorf("orthanc c-get bad status %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func (a *App) waitForStudyInOrthanc(ctx context.Context, studyUID string, pollInterval, timeout time.Duration) (bool, string, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		available, orthancStudyID, err := a.findOrthancStudy(ctx, studyUID)
		if err != nil {
			return false, "", err
		}
		if available {
			return true, orthancStudyID, nil
		}
		select {
		case <-ctx.Done():
			return false, "", ctx.Err()
		case <-time.After(pollInterval):
		}
	}
	return false, "", nil
}

func (a *App) markPatientStudyAvailableLocal(ctx context.Context, patientID, studyUID, orthancStudyID, sourceNodeID string) error {
	_, err := a.db.ExecContext(ctx, `
		UPDATE patient_study_access
		SET availability_status = 'available_local',
		    local_orthanc_study_id = $3,
		    last_seen_at = now(),
		    last_authorized_at = now(),
		    source_json = jsonb_set(
		      jsonb_set(COALESCE(source_json, '{}'::jsonb), '{source_node_id}', to_jsonb($4::text), true),
		      '{orthanc_study_id}', to_jsonb($3::text), true
		    )
		WHERE patient_id = $1::uuid
		  AND study_instance_uid = $2
	`, patientID, studyUID, orthancStudyID, sourceNodeID)
	return err
}

func (a *App) upsertCachedStudy(ctx context.Context, studyUID, orthancStudyID string, locations []string, cacheStatus string) error {
	locationsJSON, err := json.Marshal(locations)
	if err != nil {
		return err
	}
	_, err = a.db.ExecContext(ctx, `
		INSERT INTO cached_studies (
			study_instance_uid, orthanc_study_id, first_seen_at, last_verified_at, cache_status, locations_json
		) VALUES (
			$1, $2, now(), now(), $3, $4::jsonb
		)
		ON CONFLICT (study_instance_uid) DO UPDATE SET
			orthanc_study_id = EXCLUDED.orthanc_study_id,
			last_verified_at = now(),
			cache_status = EXCLUDED.cache_status,
			locations_json = EXCLUDED.locations_json
	`, studyUID, orthancStudyID, cacheStatus, string(locationsJSON))
	return err
}

func dicomFirstString(item qidoResponseItem, tag string) string {
	attribute, ok := item[tag]
	if !ok || len(attribute.Value) == 0 {
		return ""
	}

	var direct string
	if err := json.Unmarshal(attribute.Value[0], &direct); err == nil {
		return strings.TrimSpace(direct)
	}

	var named struct {
		Alphabetic string `json:"Alphabetic"`
	}
	if err := json.Unmarshal(attribute.Value[0], &named); err == nil {
		return strings.TrimSpace(named.Alphabetic)
	}

	return ""
}

func dicomFirstPersonName(item qidoResponseItem, tag string) string {
	return dicomFirstString(item, tag)
}

func normalizeStudyDate(value string) string {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) == 8 && !strings.Contains(trimmed, "-") {
		if parsed, err := time.Parse("20060102", trimmed); err == nil {
			return parsed.Format("2006-01-02")
		}
	}
	return trimmed
}

func dicomStringList(item qidoResponseItem, tag string) []string {
	attribute, ok := item[tag]
	if !ok || len(attribute.Value) == 0 {
		return nil
	}

	values := make([]string, 0, len(attribute.Value))
	for _, raw := range attribute.Value {
		var direct string
		if err := json.Unmarshal(raw, &direct); err == nil {
			direct = strings.TrimSpace(direct)
			if direct != "" {
				values = append(values, direct)
			}
		}
	}

	return values
}

func (a *App) ensurePhysicianRecord(ctx context.Context, username string) (PhysicianSummary, error) {
	var physician PhysicianSummary

	identity, err := a.professionalIdentitySource.ResolveByUsername(ctx, username)
	if err != nil {
		return PhysicianSummary{}, err
	}
	if !identity.Licensed {
		return PhysicianSummary{}, ErrProfessionalNotLicensed
	}
	dni := identity.DNI
	if dni == "" {
		dni = digitsOnly(username)
		if dni == "" {
			dni = username
		}
	}

	err = a.db.QueryRowContext(ctx, `
		INSERT INTO physicians (username, dni, full_name, license_number, licensed, auth_provider, mfa_enabled, last_login_at, last_success_auth_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, true, now(), now(), now())
		ON CONFLICT (username) DO UPDATE SET
			dni = EXCLUDED.dni,
			full_name = EXCLUDED.full_name,
			license_number = EXCLUDED.license_number,
			licensed = EXCLUDED.licensed,
			last_login_at = now(),
			last_success_auth_at = now(),
			updated_at = now()
		RETURNING id::text, username, COALESCE(dni, ''), COALESCE(full_name, ''), COALESCE(license_number, '')
	`,
		username,
		dni,
		identity.FullName,
		identity.LicenseNumber,
		identity.Licensed,
		identity.SourceSystem,
	).Scan(&physician.ID, &physician.Username, &physician.DNI, &physician.FullName, &physician.LicenseNumber)
	if err != nil {
		return PhysicianSummary{}, fmt.Errorf("upsert physician: %w", err)
	}

	return physician, nil
}

func (a *App) searchPhysicianResultsFromSingleNode(ctx context.Context, physician PhysicianSummary, filters PhysicianSearchFilters) ([]PhysicianResult, error) {
	if len(a.externalConfig.PACSNodes) != 1 {
		return nil, fmt.Errorf("physician qido flow requires exactly one pacs node, found %d", len(a.externalConfig.PACSNodes))
	}

	node := a.externalConfig.PACSNodes[0]
	if strings.ToLower(node.Protocol) != "qido_rs" {
		return nil, fmt.Errorf("physician qido flow requires qido_rs node, found %s", node.Protocol)
	}

	searchStartedAt := time.Now()
	a.log("info", "physician_qido_search_started", map[string]any{
		"physician_id": physician.ID,
		"username":     physician.Username,
		"node_id":      node.ID,
		"patient_id":   filters.PatientID,
		"patient_name": filters.PatientName,
		"date_from":    filters.DateFrom,
		"date_to":      filters.DateTo,
		"modality":     filters.Modality,
	})

	token, err := a.fetchPACSBearerToken(ctx, node)
	if err != nil {
		return nil, fmt.Errorf("fetch pacs token for %s: %w", node.ID, err)
	}

	endpoint, err := url.Parse(strings.TrimRight(node.DICOMwebBaseURL, "/") + "/studies")
	if err != nil {
		return nil, fmt.Errorf("build qido url: %w", err)
	}

	query := endpoint.Query()
	query.Set("limit", "50")
	query.Add("includefield", "StudyInstanceUID")
	query.Add("includefield", "StudyDate")
	query.Add("includefield", "StudyDescription")
	query.Add("includefield", "ModalitiesInStudy")
	query.Add("includefield", "PatientName")
	query.Add("includefield", "PatientID")
	if filters.PatientID != "" {
		query.Set("PatientID", filters.PatientID)
	}
	if filters.PatientName != "" {
		query.Set("PatientName", buildPatientNameFuzzyQuery(filters.PatientName))
	}
	if filters.Modality != "" {
		query.Set("ModalitiesInStudy", filters.Modality)
	}
	if filters.DateFrom != "" || filters.DateTo != "" {
		query.Set("StudyDate", buildQIDODateRange(filters.DateFrom, filters.DateTo))
	}
	endpoint.RawQuery = query.Encode()

	a.log("info", "physician_qido_request_started", map[string]any{
		"physician_id": physician.ID,
		"username":     physician.Username,
		"node_id":      node.ID,
		"url":          endpoint.String(),
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build physician qido request: %w", err)
	}
	req.Header.Set("Accept", "application/dicom+json, application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	res, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute physician qido request: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return nil, fmt.Errorf("physician qido bad status %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload []qidoResponseItem
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		if !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("decode physician qido response: %w", err)
		}
		payload = []qidoResponseItem{}
	}

	results := make([]PhysicianResult, 0, len(payload))
	for _, item := range payload {
		studyUID := dicomFirstString(item, "0020000D")
		if studyUID == "" {
			continue
		}

		result := PhysicianResult{
			StudyInstanceUID: studyUID,
			PatientName:      dicomFirstPersonName(item, "00100010"),
			PatientID:        dicomFirstString(item, "00100020"),
			StudyDate:        normalizeStudyDate(dicomFirstString(item, "00080020")),
			StudyDescription: dicomFirstString(item, "00081030"),
			Modalities:       dicomStringList(item, "00080061"),
			Locations:        []string{node.Name},
			CacheStatus:      "not_local",
			RetrieveStatus:   "idle",
			PartialFilter:    false,
		}

		cacheStatus, retrieveStatus, viewerURL, err := a.getStudyOperationalState(ctx, studyUID, result.CacheStatus, result.RetrieveStatus)
		if err != nil {
			return nil, fmt.Errorf("resolve physician qido state for %s: %w", studyUID, err)
		}
		result.CacheStatus = cacheStatus
		result.RetrieveStatus = retrieveStatus
		result.ViewerURL = viewerURL
		results = append(results, result)
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].StudyDate == results[j].StudyDate {
			return results[i].StudyInstanceUID < results[j].StudyInstanceUID
		}
		return results[i].StudyDate > results[j].StudyDate
	})

	if err := a.persistPhysicianRecentQuery(ctx, physician.ID, filters, results); err != nil {
		return nil, fmt.Errorf("persist physician recent query: %w", err)
	}

	a.log("info", "physician_qido_search_completed", map[string]any{
		"physician_id": physician.ID,
		"username":     physician.Username,
		"node_id":      node.ID,
		"result_count": len(results),
		"duration_ms":  time.Since(searchStartedAt).Milliseconds(),
	})

	return results, nil
}

func buildQIDODateRange(dateFrom, dateTo string) string {
	from := strings.ReplaceAll(strings.TrimSpace(dateFrom), "-", "")
	to := strings.ReplaceAll(strings.TrimSpace(dateTo), "-", "")
	switch {
	case from != "" && to != "":
		return from + "-" + to
	case from != "":
		return from + "-"
	case to != "":
		return "-" + to
	default:
		return ""
	}
}

func configuredDateRange(period string, now time.Time) (string, string) {
	switch strings.ToLower(strings.TrimSpace(period)) {
	case "", "current_week", "week":
		return currentWeekDateRange(now)
	case "today":
		year, month, day := now.Date()
		current := time.Date(year, month, day, 0, 0, 0, 0, now.Location())
		dayISO := current.Format("2006-01-02")
		return dayISO, dayISO
	case "current_month", "month":
		year, month, _ := now.Date()
		start := time.Date(year, month, 1, 0, 0, 0, 0, now.Location())
		end := start.AddDate(0, 1, -1)
		return start.Format("2006-01-02"), end.Format("2006-01-02")
	case "current_year", "year":
		year, _, _ := now.Date()
		start := time.Date(year, time.January, 1, 0, 0, 0, 0, now.Location())
		end := time.Date(year, time.December, 31, 0, 0, 0, 0, now.Location())
		return start.Format("2006-01-02"), end.Format("2006-01-02")
	default:
		return currentWeekDateRange(now)
	}
}

func currentWeekDateRange(now time.Time) (string, string) {
	year, month, day := now.Date()
	current := time.Date(year, month, day, 0, 0, 0, 0, now.Location())
	offset := (int(current.Weekday()) + 6) % 7
	start := current.AddDate(0, 0, -offset)
	end := start.AddDate(0, 0, 6)
	return start.Format("2006-01-02"), end.Format("2006-01-02")
}

func (a *App) persistPhysicianRecentQuery(ctx context.Context, physicianID string, filters PhysicianSearchFilters, results []PhysicianResult) error {
	payload := map[string]any{
		"patient_id":   filters.PatientID,
		"patient_name": filters.PatientName,
		"date_from":    filters.DateFrom,
		"date_to":      filters.DateTo,
		"modalities":   []string{},
		"results":      results,
	}
	if filters.Modality != "" {
		payload["modalities"] = []string{filters.Modality}
	}

	queryJSON, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	_, err = a.db.ExecContext(ctx, `
		INSERT INTO physician_recent_queries (
			physician_id, query_json, result_count, searched_at, expires_at
		) VALUES (
			$1::uuid, $2::jsonb, $3, now(), now() + interval '7 days'
		)
	`, physicianID, string(queryJSON), len(results))
	return err
}

func (a *App) listPhysicianCachedResultsForInitialPeriod(ctx context.Context) ([]PhysicianResult, error) {
	period := "current_week"
	if a.externalConfig != nil && strings.TrimSpace(a.externalConfig.Professional.InitialCachePeriod) != "" {
		period = a.externalConfig.Professional.InitialCachePeriod
	}
	dateFrom, dateTo := configuredDateRange(period, time.Now())

	endpoint, err := url.Parse(strings.TrimRight(a.cfg.OrthancURL, "/") + "/dicom-web/studies")
	if err != nil {
		return nil, fmt.Errorf("build orthanc physician cache url: %w", err)
	}

	query := endpoint.Query()
	query.Set("limit", "200")
	query.Set("StudyDate", buildQIDODateRange(dateFrom, dateTo))
	query.Add("includefield", "StudyInstanceUID")
	query.Add("includefield", "StudyDate")
	query.Add("includefield", "StudyDescription")
	query.Add("includefield", "ModalitiesInStudy")
	query.Add("includefield", "PatientName")
	query.Add("includefield", "PatientID")
	endpoint.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build orthanc physician cache request: %w", err)
	}
	req.Header.Set("Accept", "application/dicom+json, application/json")
	if a.cfg.OrthancUser != "" {
		req.SetBasicAuth(a.cfg.OrthancUser, a.cfg.OrthancPass)
	}

	res, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute orthanc physician cache request: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return nil, fmt.Errorf("orthanc physician cache bad status %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload []qidoResponseItem
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		if !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("decode orthanc physician cache response: %w", err)
		}
		payload = []qidoResponseItem{}
	}

	results := make([]PhysicianResult, 0, len(payload))
	for _, item := range payload {
		studyUID := dicomFirstString(item, "0020000D")
		if studyUID == "" {
			continue
		}

		cacheStatus, retrieveStatus, viewerURL, err := a.getStudyOperationalState(ctx, studyUID, "local_complete", "done")
		if err != nil {
			return nil, fmt.Errorf("resolve physician cached study state for %s: %w", studyUID, err)
		}
		locations, err := a.cachedStudyLocations(ctx, studyUID)
		if err != nil {
			return nil, fmt.Errorf("load cached study locations for %s: %w", studyUID, err)
		}
		if len(locations) == 0 {
			locations = []string{"Cache local"}
		}

		results = append(results, PhysicianResult{
			StudyInstanceUID: studyUID,
			PatientName:      dicomFirstPersonName(item, "00100010"),
			PatientID:        dicomFirstString(item, "00100020"),
			StudyDate:        normalizeStudyDate(dicomFirstString(item, "00080020")),
			StudyDescription: dicomFirstString(item, "00081030"),
			Modalities:       dicomStringList(item, "00080061"),
			Locations:        locations,
			CacheStatus:      cacheStatus,
			RetrieveStatus:   retrieveStatus,
			PartialFilter:    false,
			ViewerURL:        viewerURL,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].StudyDate == results[j].StudyDate {
			return results[i].StudyInstanceUID < results[j].StudyInstanceUID
		}
		return results[i].StudyDate > results[j].StudyDate
	})

	return results, nil
}

func (a *App) cachedStudyLocations(ctx context.Context, studyUID string) ([]string, error) {
	var raw []byte
	err := a.db.QueryRowContext(ctx, `
		SELECT locations_json
		FROM cached_studies
		WHERE study_instance_uid = $1
	`, studyUID).Scan(&raw)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	var locations []string
	if len(raw) == 0 {
		return nil, nil
	}
	if err := json.Unmarshal(raw, &locations); err != nil {
		return nil, err
	}
	return locations, nil
}

func (a *App) listPatientStudies(ctx context.Context, patientID string, filters PatientStudiesFilter) ([]PatientStudy, error) {
	query := `
		SELECT
			study_instance_uid,
			availability_status,
			authorization_basis,
			source_json
		FROM patient_study_access
		WHERE patient_id = $1::uuid
	`

	args := []any{patientID}
	position := 2

	if filters.DateFrom != "" {
		query += fmt.Sprintf(` AND REPLACE(COALESCE(source_json->>'study_date', ''), '-', '') >= REPLACE($%d, '-', '')`, position)
		args = append(args, filters.DateFrom)
		position++
	}
	if filters.DateTo != "" {
		query += fmt.Sprintf(` AND REPLACE(COALESCE(source_json->>'study_date', ''), '-', '') <= REPLACE($%d, '-', '')`, position)
		args = append(args, filters.DateTo)
		position++
	}
	if filters.Modality != "" {
		query += fmt.Sprintf(` AND EXISTS (
			SELECT 1
			FROM jsonb_array_elements_text(COALESCE(source_json->'modalities_in_study', '[]'::jsonb)) AS modality
			WHERE UPPER(modality) = $%d
		)`, position)
		args = append(args, filters.Modality)
		position++
	}

	query += ` ORDER BY COALESCE(source_json->>'study_date', '') DESC, study_instance_uid ASC`

	rows, err := a.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	studies := make([]PatientStudy, 0)
	for rows.Next() {
		var (
			studyUID            string
			availabilityStatus  string
			authorizationBasis  string
			sourceJSONRaw       []byte
		)

		if err := rows.Scan(&studyUID, &availabilityStatus, &authorizationBasis, &sourceJSONRaw); err != nil {
			return nil, err
		}

		var source struct {
			StudyDate         string   `json:"study_date"`
			StudyDescription  string   `json:"study_description"`
			ModalitiesInStudy []string `json:"modalities_in_study"`
		}
		if len(sourceJSONRaw) > 0 {
			if err := json.Unmarshal(sourceJSONRaw, &source); err != nil {
				return nil, fmt.Errorf("parse patient study source_json: %w", err)
			}
		}

		study := PatientStudy{
			StudyInstanceUID:   studyUID,
			StudyDate:          source.StudyDate,
			StudyDescription:   source.StudyDescription,
			ModalitiesInStudy:  source.ModalitiesInStudy,
			AvailabilityStatus: availabilityStatus,
			RetrieveStatus:     "idle",
			AuthorizationBasis: authorizationBasis,
		}
		cacheStatus := "not_local"
		if availabilityStatus == "available_local" {
			cacheStatus = "local_complete"
		}
		_, retrieveStatus, viewerURL, err := a.getStudyOperationalState(ctx, studyUID, cacheStatus, study.RetrieveStatus)
		if err != nil {
			return nil, fmt.Errorf("resolve patient study operational state for %s: %w", studyUID, err)
		}
		study.RetrieveStatus = retrieveStatus
		study.ViewerURL = viewerURL

		studies = append(studies, study)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return studies, nil
}

func (a *App) listPhysicianResults(ctx context.Context, physicianID string, filters PhysicianSearchFilters) ([]PhysicianResult, error) {
	if hasPhysicianFilters(filters) {
		physician := PhysicianSummary{ID: physicianID}
		if err := a.db.QueryRowContext(ctx, `
			SELECT username, COALESCE(dni, ''), COALESCE(full_name, '')
			FROM physicians
			WHERE id = $1::uuid
		`, physicianID).Scan(&physician.Username, &physician.DNI, &physician.FullName); err != nil {
			return nil, fmt.Errorf("load physician summary: %w", err)
		}
		return a.searchPhysicianResultsFromSingleNode(ctx, physician, filters)
	}

	return a.listPhysicianCachedResultsForInitialPeriod(ctx)
}

func digitsOnly(value string) string {
	var out strings.Builder
	for _, r := range value {
		if r >= '0' && r <= '9' {
			out.WriteRune(r)
		}
	}
	return out.String()
}

func buildOHIFViewerURL(studyInstanceUID string) string {
	return "/ohif/viewer?StudyInstanceUIDs=" + url.QueryEscape(strings.TrimSpace(studyInstanceUID))
}

func validateExternalConfig(cfg ExternalConfig) error {
	if len(cfg.PACSNodes) == 0 {
		return errors.New("config must include at least one PACS node")
	}

	for _, node := range cfg.PACSNodes {
		if strings.TrimSpace(node.ID) == "" {
			return errors.New("pacs node id is required")
		}
		if strings.TrimSpace(node.Name) == "" {
			return fmt.Errorf("pacs node %q name is required", node.ID)
		}
		if strings.TrimSpace(node.Protocol) == "" {
			return fmt.Errorf("pacs node %q protocol is required", node.ID)
		}
		if strings.TrimSpace(node.DICOMwebBaseURL) == "" {
			return fmt.Errorf("pacs node %q dicomweb_base_url is required", node.ID)
		}

		if node.Auth.Type == "keycloak_client_credentials" {
			if strings.TrimSpace(node.Auth.TokenURL) == "" {
				return fmt.Errorf("pacs node %q token_url is required", node.ID)
			}
			if strings.TrimSpace(node.Auth.ClientIDEnv) == "" || strings.TrimSpace(node.Auth.ClientSecretEnv) == "" {
				return fmt.Errorf("pacs node %q client env refs are required", node.ID)
			}
			if strings.TrimSpace(os.Getenv(node.Auth.ClientIDEnv)) == "" {
				return fmt.Errorf("pacs node %q missing env value for %s", node.ID, node.Auth.ClientIDEnv)
			}
			if strings.TrimSpace(os.Getenv(node.Auth.ClientSecretEnv)) == "" {
				return fmt.Errorf("pacs node %q missing env value for %s", node.ID, node.Auth.ClientSecretEnv)
			}
		}
	}

	return nil
}

func runMigrations(ctx context.Context, db *sql.DB, dir string) ([]string, error) {
	if _, err := os.Stat(dir); err != nil {
		return nil, fmt.Errorf("stat migrations dir: %w", err)
	}

	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			filename text PRIMARY KEY,
			applied_at timestamptz NOT NULL DEFAULT now()
		)
	`); err != nil {
		return nil, fmt.Errorf("ensure schema_migrations: %w", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read migrations dir: %w", err)
	}

	var names []string
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sql" {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)

	applied := make([]string, 0, len(names))
	for _, name := range names {
		var exists bool
		if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE filename = $1)`, name).Scan(&exists); err != nil {
			return nil, fmt.Errorf("check migration %s: %w", name, err)
		}
		if exists {
			applied = append(applied, name)
			continue
		}

		path := filepath.Join(dir, name)
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", name, err)
		}

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return nil, fmt.Errorf("begin migration tx %s: %w", name, err)
		}

		if _, err := tx.ExecContext(ctx, string(content)); err != nil {
			_ = tx.Rollback()
			return nil, fmt.Errorf("exec migration %s: %w", name, err)
		}

		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (filename) VALUES ($1)`, name); err != nil {
			_ = tx.Rollback()
			return nil, fmt.Errorf("record migration %s: %w", name, err)
		}

		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit migration %s: %w", name, err)
		}

		applied = append(applied, name)
	}

	return applied, nil
}

func persistExternalConfig(ctx context.Context, db *sql.DB, cfg ExternalConfig) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, node := range cfg.PACSNodes {
		authJSON, err := json.Marshal(map[string]any{
			"type":              node.Auth.Type,
			"token_url":         node.Auth.TokenURL,
			"client_id_env":     node.Auth.ClientIDEnv,
			"client_secret_env": node.Auth.ClientSecretEnv,
		})
		if err != nil {
			return fmt.Errorf("marshal pacs auth config for %s: %w", node.ID, err)
		}

		_, err = tx.ExecContext(ctx, `
			INSERT INTO pacs_nodes (
				code, name, protocol, priority, enabled, ae_title, host, port,
				dicomweb_base_url, supports_cmove, supports_cget, auth_type, auth_config_json, updated_at
			) VALUES (
				$1, $2, $3, $4, true, $5, $6, $7, $8, $9, $10, $11, $12::jsonb, now()
			)
			ON CONFLICT (code) DO UPDATE SET
				name = EXCLUDED.name,
				protocol = EXCLUDED.protocol,
				priority = EXCLUDED.priority,
				enabled = EXCLUDED.enabled,
				ae_title = EXCLUDED.ae_title,
				host = EXCLUDED.host,
				port = EXCLUDED.port,
				dicomweb_base_url = EXCLUDED.dicomweb_base_url,
				supports_cmove = EXCLUDED.supports_cmove,
				supports_cget = EXCLUDED.supports_cget,
				auth_type = EXCLUDED.auth_type,
				auth_config_json = EXCLUDED.auth_config_json,
				updated_at = now()
		`,
			node.ID,
			node.Name,
			node.Protocol,
			node.Priority,
			node.AET,
			node.DICOMHost,
			node.DICOMPort,
			node.DICOMwebBaseURL,
			node.SupportsCMove,
			node.SupportsCGet,
			node.Auth.Type,
			string(authJSON),
		)
		if err != nil {
			return fmt.Errorf("upsert pacs node %s: %w", node.ID, err)
		}
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM his_config`); err != nil {
		return fmt.Errorf("clear his_config: %w", err)
	}

	paramsJSON, err := json.Marshal(map[string]any{
		"document_lookup_path": cfg.HIS.DocumentLookupPath,
	})
	if err != nil {
		return fmt.Errorf("marshal his params: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO his_config (
			provider, enabled, base_url, auth_type, params_json, secret_refs_json, updated_at
		) VALUES ($1, $2, $3, $4, $5::jsonb, '{}'::jsonb, now())
	`,
		cfg.HIS.Provider,
		cfg.HIS.Enabled,
		cfg.HIS.BaseURL,
		cfg.HIS.AuthType,
		string(paramsJSON),
	); err != nil {
		return fmt.Errorf("insert his_config: %w", err)
	}

	return tx.Commit()
}

func (a *App) log(level, msg string, fields map[string]any) {
	payload := map[string]any{
		"level": level,
		"msg":   msg,
		"ts":    time.Now().UTC().Format(time.RFC3339),
	}

	for k, v := range fields {
		payload[k] = v
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		a.logger.Println(fmt.Sprintf(`{"level":"error","msg":"log_marshal_failed","error":%q}`, err.Error()))
		return
	}

	a.logger.Println(string(encoded))
}

func envOrDefault(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}
