package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/go-ldap/ldap/v3"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"log"
	"math"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

var ErrPatientIdentityNotFound = errors.New("patient identity not found")
var ErrProfessionalIdentityNotFound = errors.New("professional identity not found")
var ErrProfessionalAuthUnavailable = errors.New("professional auth unavailable")
var ErrSourceNodeUnavailable = errors.New("source pacs unavailable")

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
	provider   string
	logger     *log.Logger
	retryEvery time.Duration
	build      func() (PatientIdentitySource, error)
	current    PatientIdentitySource
	currentMu  sync.RWMutex
	stopCh     chan struct{}
	stopOnce   sync.Once
	refreshMu  sync.Mutex
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

type MongoPacienteDocument struct {
	ID              primitive.ObjectID      `bson:"_id"`
	Documento       any                     `bson:"documento"`
	Nombre          string                  `bson:"nombre"`
	Apellido        string                  `bson:"apellido"`
	Alias           string                  `bson:"alias"`
	Nacionalidad    string                  `bson:"nacionalidad"`
	Sexo            string                  `bson:"sexo"`
	Genero          string                  `bson:"genero"`
	FechaNacimiento time.Time               `bson:"fechaNacimiento"`
	Contacto        []MongoPacienteContacto `bson:"contacto"`
}

type MongoProfesionalDocument struct {
	ID                     primitive.ObjectID          `bson:"_id"`
	Documento              any                         `bson:"documento"`
	Nombre                 string                      `bson:"nombre"`
	Apellido               string                      `bson:"apellido"`
	Habilitado             bool                        `bson:"habilitado"`
	ProfesionalMatriculado bool                        `bson:"profesionalMatriculado"`
	FormacionGrado         []MongoProfesionalFormacion `bson:"formacionGrado"`
}

type MongoProfesionalFormacion struct {
	Matriculacion []MongoProfesionalMatriculacion `bson:"matriculacion"`
}

type MongoProfesionalMatriculacion struct {
	MatriculaNumero any                  `bson:"matriculaNumero"`
	Baja            MongoProfesionalBaja `bson:"baja"`
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
	client            *mongo.Client
	collection        *mongo.Collection
	connectTimeout    time.Duration
	queryTimeout      time.Duration
	licenseExceptions map[string]struct{}
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

func (s *MongoPatientIdentitySource) Healthy() bool {
	ctx, cancel := context.WithTimeout(context.Background(), s.connectTimeout)
	defer cancel()
	return s.client.Ping(ctx, nil) == nil
}

func (s *MongoProfessionalIdentitySource) Healthy() bool {
	ctx, cancel := context.WithTimeout(context.Background(), s.connectTimeout)
	defer cancel()
	return s.client.Ping(ctx, nil) == nil
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
	exceptionAllowed := false
	if _, ok := s.licenseExceptions[resolvedDocument]; ok {
		exceptionAllowed = true
	}
	licensed := exceptionAllowed || (doc.Habilitado && doc.ProfesionalMatriculado && strings.TrimSpace(licenseNumber) != "")

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

func mongoValueIsNull(value any) bool {
	if value == nil {
		return true
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed) == ""
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

func (a *App) mongoIdentityComponent() (ComponentHealth, bool) {
	if a.identitySource.ProviderName() != "his_mongo_direct" && a.professionalIdentitySource.ProviderName() != "his_mongo_direct" {
		return ComponentHealth{}, false
	}

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

	status := ComponentStatusHealthy
	message := "patient and professional identity available"
	if !patientOK || !professionalOK {
		status = ComponentStatusUnavailable
		message = "patient or professional identity unavailable"
	}

	return ComponentHealth{
		Name:     "mongo_identity",
		Category: "feature_required",
		Severity: ComponentSeverityRequired,
		Status:   status,
		Message:  message,
	}, true
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
		return connectMongoProfessionalIdentitySource(cfg.Professional)
	})
}

func connectMongoPatientIdentitySource() (PatientIdentitySource, error) {
	mongoURI := strings.TrimSpace(os.Getenv("HIS_MONGO_URI"))
	mongoDatabase := strings.TrimSpace(os.Getenv("HIS_MONGO_DATABASE"))
	if mongoURI == "" || mongoDatabase == "" {
		return nil, errors.New("missing required mongo env vars for his_mongo_direct provider")
	}

	connectTimeout := durationFromEnv("HIS_MONGO_CONNECT_TIMEOUT_MS", 5000*time.Millisecond)
	queryTimeout := durationFromEnv("HIS_MONGO_QUERY_TIMEOUT_MS", 10000*time.Millisecond)

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

func connectMongoProfessionalIdentitySource(cfg ProfessionalConfig) (ProfessionalIdentitySource, error) {
	mongoURI := strings.TrimSpace(os.Getenv("HIS_MONGO_URI"))
	mongoDatabase := strings.TrimSpace(os.Getenv("HIS_MONGO_DATABASE"))
	if mongoURI == "" || mongoDatabase == "" {
		return nil, errors.New("missing required mongo env vars for his_mongo_direct provider")
	}

	connectTimeout := durationFromEnv("HIS_MONGO_CONNECT_TIMEOUT_MS", 5000*time.Millisecond)
	queryTimeout := durationFromEnv("HIS_MONGO_QUERY_TIMEOUT_MS", 10000*time.Millisecond)

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
		client:            client,
		collection:        client.Database(mongoDatabase).Collection("profesional"),
		connectTimeout:    connectTimeout,
		queryTimeout:      queryTimeout,
		licenseExceptions: normalizeExceptionSet(cfg.LicenseExceptions),
	}, nil
}

func authenticateProfessionalLDAP(_ context.Context, username, password string) error {
	ldapHost := strings.TrimSpace(os.Getenv("LDAP_HOST"))
	ldapPort := strings.TrimSpace(os.Getenv("LDAP_PORT"))
	ldapOU := strings.TrimSpace(os.Getenv("LDAP_OU"))
	if ldapHost == "" || ldapPort == "" || ldapOU == "" {
		return fmt.Errorf("%w: missing LDAP_HOST, LDAP_PORT, or LDAP_OU", ErrProfessionalAuthUnavailable)
	}
	if strings.TrimSpace(password) == "" {
		return ErrProfessionalInvalidCredentials
	}

	dialer := &net.Dialer{Timeout: 4 * time.Second}
	conn, err := ldap.DialURL("ldap://"+net.JoinHostPort(ldapHost, ldapPort), ldap.DialWithDialer(dialer))
	if err != nil {
		return fmt.Errorf("%w: dial ldap: %v", ErrProfessionalAuthUnavailable, err)
	}
	defer conn.Close()

	conn.SetTimeout(4 * time.Second)
	dn := "uid=" + strings.TrimSpace(username) + "," + ldapOU
	if err := conn.Bind(dn, password); err != nil {
		var ldapErr *ldap.Error
		if errors.As(err, &ldapErr) {
			if ldapErr.ResultCode == ldap.LDAPResultInvalidCredentials || ldapErr.ResultCode == ldap.LDAPResultNoSuchObject {
				return ErrProfessionalInvalidCredentials
			}
		}
		return fmt.Errorf("%w: bind ldap: %v", ErrProfessionalAuthUnavailable, err)
	}

	return nil
}

func (a *App) logPatientIdentityComparison(patient PatientSummary, candidate remotePatientMatchCandidate) {
	if !a.shouldLogPatientMatchDebug(candidate.NodeID) {
		return
	}
	hisDocument := strings.TrimSpace(patient.DocumentNumber)
	remotePatientID := strings.TrimSpace(candidate.PatientID)
	hisName := normalizeFuzzySearchText(patient.FullName)
	remoteName := normalizeFuzzySearchText(candidate.PatientName)
	hisBirthDate := strings.TrimSpace(patient.BirthDate)
	remoteBirthDate := normalizeRemoteBirthDate(candidate.BirthDate)
	hisSex := normalizeRemoteSex(patient.Sex)
	remoteSex := normalizeRemoteSex(candidate.Sex)

	a.log("info", "patient_identity_match_probe", map[string]any{
		"node_id":                     candidate.NodeID,
		"study_instance_uid":          candidate.StudyInstanceUID,
		"his_document_number":         hisDocument,
		"remote_patient_id":           remotePatientID,
		"document_matches_patient_id": hisDocument != "" && remotePatientID != "" && hisDocument == remotePatientID,
		"his_full_name":               patient.FullName,
		"remote_patient_name":         candidate.PatientName,
		"normalized_name_match":       hisName != "" && remoteName != "" && hisName == remoteName,
		"his_birth_date":              hisBirthDate,
		"remote_birth_date":           remoteBirthDate,
		"birth_date_match":            hisBirthDate != "" && remoteBirthDate != "" && hisBirthDate == remoteBirthDate,
		"his_sex":                     hisSex,
		"remote_sex":                  remoteSex,
		"sex_match":                   hisSex != "" && remoteSex != "" && hisSex == remoteSex,
		"demographic_match":           patientDemographicMatch(patient, candidate),
	})
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

func (a *App) loadPatientMongoObjectID(ctx context.Context, patientID string) (string, error) {
	var mongoObjectID string
	err := a.db.QueryRowContext(ctx, `
		SELECT identifier_value
		FROM patient_identifiers
		WHERE patient_id = $1::uuid
		  AND identifier_type = 'mongo_object_id'
		ORDER BY last_verified_at DESC, updated_at DESC
		LIMIT 1
	`, patientID).Scan(&mongoObjectID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(mongoObjectID), nil
}

func normalizeMongoObjectIDCandidate(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if _, err := primitive.ObjectIDFromHex(value); err != nil {
		return ""
	}
	return strings.ToLower(value)
}

func mongoObjectIDFromAlternateIdentifiers(identifiers []PatientAlternateIdentifier) string {
	for _, identifier := range identifiers {
		if !strings.EqualFold(strings.TrimSpace(identifier.Type), "mongo_object_id") {
			continue
		}
		if mongoID := normalizeMongoObjectIDCandidate(identifier.Value); mongoID != "" {
			return mongoID
		}
	}
	return ""
}

func (a *App) loadCachedMongoObjectIDByDocument(ctx context.Context, documentNumber string) (string, error) {
	documentNumber = normalizeDocumentNumberCandidate(documentNumber)
	if documentNumber == "" {
		return "", nil
	}

	var mongoObjectID string
	err := a.db.QueryRowContext(ctx, `
		SELECT mongo.identifier_value
		FROM patient_identifiers AS doc
		JOIN patient_identifiers AS mongo
			ON mongo.patient_id = doc.patient_id
			AND mongo.identifier_type = 'mongo_object_id'
		WHERE doc.identifier_type = 'document_number'
		  AND doc.identifier_value = $1
		ORDER BY mongo.is_primary DESC, mongo.last_verified_at DESC, mongo.updated_at DESC
		LIMIT 1
	`, documentNumber).Scan(&mongoObjectID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("load cached mongo id by document: %w", err)
	}
	return normalizeMongoObjectIDCandidate(mongoObjectID), nil
}
