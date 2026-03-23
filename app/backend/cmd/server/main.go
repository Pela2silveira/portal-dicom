package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
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
}

type HealthResponse struct {
	Status     string `json:"status"`
	AppEnv     string `json:"app_env"`
	DBOK       bool   `json:"db_ok"`
	OrthancOK  bool   `json:"orthanc_ok"`
	ConfigOK   bool   `json:"config_ok"`
	CheckedAt  string `json:"checked_at"`
	ConfigPath string `json:"config_path"`
}

type PatientStudiesResponse struct {
	DocumentNumber string               `json:"document_number"`
	Patient        PatientSummary       `json:"patient"`
	Filters        PatientStudiesFilter `json:"filters"`
	Studies        []PatientStudy       `json:"studies"`
}

type PatientSummary struct {
	ID             string `json:"id"`
	DocumentType   string `json:"document_type"`
	DocumentNumber string `json:"document_number"`
	FullName       string `json:"full_name"`
}

type PatientStudiesFilter struct {
	DateFrom string `json:"date_from,omitempty"`
	DateTo   string `json:"date_to,omitempty"`
	Modality string `json:"modality,omitempty"`
}

type PatientStudy struct {
	StudyInstanceUID   string   `json:"study_instance_uid"`
	StudyDate          string   `json:"study_date"`
	StudyDescription   string   `json:"study_description"`
	ModalitiesInStudy  []string `json:"modalities_in_study"`
	AvailabilityStatus string   `json:"availability_status"`
	AuthorizationBasis string   `json:"authorization_basis"`
	ViewerURL          string   `json:"viewer_url,omitempty"`
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

type PhysicianSummary struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	DNI      string `json:"dni"`
	FullName string `json:"full_name"`
}

type PhysicianSearchFilters struct {
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
	Cache      CacheConfig        `json:"cache"`
	Migrations []string           `json:"migrations"`
}

type ExternalConfig struct {
	PACSNodes []PACSNodeConfig `json:"pacs_nodes"`
	HIS       HISConfig        `json:"his"`
	Cache     CacheConfig      `json:"cache"`
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

	if cfg.PostgresDSN == "" {
		logger.Fatal(`missing required env var "POSTGRES_DSN"`)
	}
	if cfg.OrthancURL == "" {
		logger.Fatal(`missing required env var "ORTHANC_URL"`)
	}

	db, err := sql.Open("pgx", cfg.PostgresDSN)
	if err != nil {
		logger.Fatal(err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		logger.Fatal(err)
	}

	appliedMigrations, err := runMigrations(ctx, db, cfg.MigrationsDir)
	if err != nil {
		logger.Fatal(err)
	}

	externalConfig, err := loadExternalConfig(cfg.ConfigPath)
	if err != nil {
		logger.Fatal(err)
	}

	if err := validateExternalConfig(*externalConfig); err != nil {
		logger.Fatal(err)
	}

	if err := persistExternalConfig(ctx, db, *externalConfig); err != nil {
		logger.Fatal(err)
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
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", app.handleHealth)
	mux.HandleFunc("/api/config", app.handleConfig(appliedMigrations))
	mux.HandleFunc("/api/patient/studies", app.handlePatientStudies)
	mux.HandleFunc("/api/physician/results", app.handlePhysicianResults)

	app.log("info", "server_starting", map[string]any{
		"listen_addr":        cfg.ListenAddr,
		"app_env":            cfg.AppEnv,
		"log_level":          cfg.LogLevel,
		"config_path":        cfg.ConfigPath,
		"migrations_dir":     cfg.MigrationsDir,
		"migrations_applied": len(appliedMigrations),
		"pacs_nodes_loaded":  len(externalConfig.PACSNodes),
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
		Status:     "ok",
		AppEnv:     a.cfg.AppEnv,
		DBOK:       a.checkDB(ctx),
		OrthancOK:  a.checkOrthanc(ctx),
		ConfigOK:   a.checkConfig(),
		CheckedAt:  time.Now().UTC().Format(time.RFC3339),
		ConfigPath: a.cfg.ConfigPath,
	}

	if !resp.DBOK || !resp.OrthancOK || !resp.ConfigOK {
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
		"status_code": statusCode,
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
			Cache:      a.externalConfig.Cache,
			Migrations: appliedMigrations,
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

	if filters.DateFrom == "" && filters.DateTo == "" && filters.Modality == "" {
		patient, err = a.syncPatientStudiesFromSingleNode(ctx, patient, documentNumber)
		if err != nil {
			a.log("error", "patient_qido_sync_failed", map[string]any{
				"document_number": documentNumber,
				"patient_id":      patient.ID,
				"error":           err.Error(),
			})
			http.Error(w, "failed to load patient studies from remote pacs", http.StatusBadGateway)
			return
		}
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
		Studies:        studies,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (a *App) handlePhysicianResults(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	username := strings.TrimSpace(r.URL.Query().Get("username"))
	if username == "" {
		http.Error(w, "missing required query param: username", http.StatusBadRequest)
		return
	}

	filters := PhysicianSearchFilters{
		PatientName: strings.TrimSpace(r.URL.Query().Get("patient_name")),
		DateFrom:    strings.TrimSpace(r.URL.Query().Get("date_from")),
		DateTo:      strings.TrimSpace(r.URL.Query().Get("date_to")),
		Modality:    strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("modality"))),
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	physician, err := a.ensurePhysicianSeed(ctx, username)
	if err != nil {
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

func (a *App) checkDB(ctx context.Context) bool {
	if err := a.db.PingContext(ctx); err != nil {
		a.log("error", "db_ping_failed", map[string]any{"error": err.Error()})
		return false
	}
	return true
}

func (a *App) checkOrthanc(ctx context.Context) bool {
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

	var cfg ExternalConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parse config json: %w", err)
	}

	return &cfg, nil
}

func (a *App) ensurePatientRecord(ctx context.Context, documentNumber string) (PatientSummary, error) {
	var patient PatientSummary

	err := a.db.QueryRowContext(ctx, `
		INSERT INTO patients (document_type, document_number, full_name, last_login_at, updated_at)
		VALUES ('dni', $1, $2, now(), now())
		ON CONFLICT (document_type, document_number) DO UPDATE SET
			last_login_at = now(),
			updated_at = now()
		RETURNING id::text, document_type, document_number, COALESCE(full_name, '')
	`,
		documentNumber,
		"Paciente "+documentNumber,
	).Scan(&patient.ID, &patient.DocumentType, &patient.DocumentNumber, &patient.FullName)
	if err != nil {
		return PatientSummary{}, fmt.Errorf("upsert patient: %w", err)
	}

	if _, err := a.db.ExecContext(ctx, `
		INSERT INTO patient_identifiers (
			patient_id, source_system, identifier_type, identifier_value, is_primary, last_verified_at, metadata_json, updated_at
		) VALUES (
			$1::uuid, 'landing_mock', 'document_number', $2, true, now(), '{}'::jsonb, now()
		)
		ON CONFLICT (source_system, identifier_type, identifier_value) DO UPDATE SET
			patient_id = EXCLUDED.patient_id,
			is_primary = true,
			last_verified_at = now(),
			updated_at = now()
	`,
		patient.ID,
		documentNumber,
	); err != nil {
		return PatientSummary{}, fmt.Errorf("upsert patient identifier: %w", err)
	}

	return patient, nil
}

func (a *App) syncPatientStudiesFromSingleNode(ctx context.Context, patient PatientSummary, documentNumber string) (PatientSummary, error) {
	if len(a.externalConfig.PACSNodes) != 1 {
		return patient, fmt.Errorf("patient qido flow requires exactly one pacs node, found %d", len(a.externalConfig.PACSNodes))
	}

	node := a.externalConfig.PACSNodes[0]
	if strings.ToLower(node.Protocol) != "qido_rs" {
		return patient, fmt.Errorf("patient qido flow requires qido_rs node, found %s", node.Protocol)
	}

	remoteStudies, fullName, err := a.fetchPatientStudiesFromQIDO(ctx, node, documentNumber)
	if err != nil {
		return patient, err
	}

	if fullName != "" && fullName != patient.FullName {
		if _, err := a.db.ExecContext(ctx, `
			UPDATE patients
			SET full_name = $2, updated_at = now()
			WHERE id = $1::uuid
		`, patient.ID, fullName); err != nil {
			return patient, fmt.Errorf("update patient full name: %w", err)
		}
		patient.FullName = fullName
	}

	if _, err := a.db.ExecContext(ctx, `
		DELETE FROM patient_study_access
		WHERE patient_id = $1::uuid
	`, patient.ID); err != nil {
		return patient, fmt.Errorf("clear patient study access: %w", err)
	}

	for _, study := range remoteStudies {
		sourceJSON, err := json.Marshal(map[string]any{
			"study_date":          study.StudyDate,
			"study_description":   study.StudyDescription,
			"modalities_in_study": study.ModalitiesInStudy,
			"source_node_id":      node.ID,
		})
		if err != nil {
			return patient, fmt.Errorf("marshal patient qido study: %w", err)
		}

		availabilityStatus := "remote_available"
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
		`,
			patient.ID,
			study.StudyInstanceUID,
			availabilityStatus,
			string(sourceJSON),
		); err != nil {
			return patient, fmt.Errorf("insert qido-backed patient study access: %w", err)
		}
	}

	return patient, nil
}

func (a *App) fetchPatientStudiesFromQIDO(ctx context.Context, node PACSNodeConfig, documentNumber string) ([]PatientStudy, string, error) {
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
	query.Set("limit", "50")
	query.Add("includefield", "StudyInstanceUID")
	query.Add("includefield", "StudyDate")
	query.Add("includefield", "StudyDescription")
	query.Add("includefield", "ModalitiesInStudy")
	query.Add("includefield", "PatientName")
	endpoint.RawQuery = query.Encode()

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
		return nil, "", fmt.Errorf("decode qido response: %w", err)
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
			StudyDate:          dicomFirstString(item, "00080020"),
			StudyDescription:   dicomFirstString(item, "00081030"),
			ModalitiesInStudy:  dicomStringList(item, "00080061"),
			AvailabilityStatus: "remote_available",
			AuthorizationBasis: "patient_document_qido_match",
		}

		cached, err := a.isStudyAvailableLocal(ctx, studyUID)
		if err != nil {
			return nil, "", fmt.Errorf("check local cache for study %s: %w", studyUID, err)
		}
		if cached {
			study.AvailabilityStatus = "available_local"
			study.ViewerURL = "/ohif/"
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

	return payload.AccessToken, nil
}

func (a *App) isStudyAvailableLocal(ctx context.Context, studyUID string) (bool, error) {
	endpoint := strings.TrimRight(a.cfg.OrthancURL, "/") + "/dicom-web/studies?StudyInstanceUID=" + url.QueryEscape(studyUID) + "&limit=1"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false, fmt.Errorf("build orthanc qido request: %w", err)
	}
	req.Header.Set("Accept", "application/dicom+json, application/json")
	if a.cfg.OrthancUser != "" {
		req.SetBasicAuth(a.cfg.OrthancUser, a.cfg.OrthancPass)
	}

	res, err := a.httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("execute orthanc qido request: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return false, fmt.Errorf("orthanc qido bad status %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload []qidoResponseItem
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return false, fmt.Errorf("decode orthanc qido response: %w", err)
	}

	return len(payload) > 0, nil
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

func (a *App) ensurePhysicianSeed(ctx context.Context, username string) (PhysicianSummary, error) {
	var physician PhysicianSummary
	dni := digitsOnly(username)
	if dni == "" {
		dni = username
	}

	err := a.db.QueryRowContext(ctx, `
		INSERT INTO physicians (username, dni, full_name, auth_provider, mfa_enabled, last_login_at, last_success_auth_at, updated_at)
		VALUES ($1, $2, $3, 'ldap_provincial', true, now(), now(), now())
		ON CONFLICT (username) DO UPDATE SET
			dni = EXCLUDED.dni,
			full_name = EXCLUDED.full_name,
			last_login_at = now(),
			last_success_auth_at = now(),
			updated_at = now()
		RETURNING id::text, username, COALESCE(dni, ''), COALESCE(full_name, '')
	`,
		username,
		dni,
		"Profesional "+username,
	).Scan(&physician.ID, &physician.Username, &physician.DNI, &physician.FullName)
	if err != nil {
		return PhysicianSummary{}, fmt.Errorf("upsert physician: %w", err)
	}

	seedQueries := []struct {
		QueryJSON    map[string]any
		ResultCount  int
		ExpiresAfter string
	}{
		{
			QueryJSON: map[string]any{
				"patient_name": "Perez Juan",
				"date_from":    "2026-03-01",
				"date_to":      "2026-03-31",
				"modalities":   []string{"MR"},
				"results": []map[string]any{
					{
						"study_instance_uid": "1.2.826.0.1.3680043.10.54321.mr.101",
						"patient_name":       "Perez Juan",
						"patient_id":         "12345678",
						"study_date":         "2026-03-21",
						"study_description":  "RM de columna lumbar",
						"modalities":         []string{"MR"},
						"locations":          []string{"HPN", "Castro Rendon"},
						"cache_status":       "local_complete",
						"retrieve_status":    "done",
						"partial_filter":     false,
					},
				},
			},
			ResultCount:  1,
			ExpiresAfter: "7 days",
		},
		{
			QueryJSON: map[string]any{
				"patient_name": "Gomez Ana",
				"date_from":    "2026-03-15",
				"date_to":      "2026-03-31",
				"modalities":   []string{"CT"},
				"results": []map[string]any{
					{
						"study_instance_uid": "1.2.826.0.1.3680043.10.54321.ct.102",
						"patient_name":       "Gomez Ana",
						"patient_id":         "20111222",
						"study_date":         "2026-03-19",
						"study_description":  "TC de abdomen",
						"modalities":         []string{"CT"},
						"locations":          []string{"HPN"},
						"cache_status":       "local_partial",
						"retrieve_status":    "running",
						"partial_filter":     true,
					},
				},
			},
			ResultCount:  1,
			ExpiresAfter: "7 days",
		},
	}

	for _, query := range seedQueries {
		queryJSON, err := json.Marshal(query.QueryJSON)
		if err != nil {
			return PhysicianSummary{}, fmt.Errorf("marshal physician query seed: %w", err)
		}

		var exists bool
		if err := a.db.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM physician_recent_queries
				WHERE physician_id = $1::uuid
				  AND query_json = $2::jsonb
			)
		`,
			physician.ID,
			string(queryJSON),
		).Scan(&exists); err != nil {
			return PhysicianSummary{}, fmt.Errorf("check physician recent query seed: %w", err)
		}

		if exists {
			continue
		}

		if _, err := a.db.ExecContext(ctx, `
			INSERT INTO physician_recent_queries (
				physician_id, query_json, result_count, searched_at, expires_at
			) VALUES (
				$1::uuid, $2::jsonb, $3, now(), now() + ($4)::interval
			)
		`,
			physician.ID,
			string(queryJSON),
			query.ResultCount,
			query.ExpiresAfter,
		); err != nil {
			return PhysicianSummary{}, fmt.Errorf("insert physician recent query: %w", err)
		}
	}

	return physician, nil
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
		query += fmt.Sprintf(` AND COALESCE(source_json->>'study_date', '') >= $%d`, position)
		args = append(args, filters.DateFrom)
		position++
	}
	if filters.DateTo != "" {
		query += fmt.Sprintf(` AND COALESCE(source_json->>'study_date', '') <= $%d`, position)
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
			AuthorizationBasis: authorizationBasis,
		}
		if availabilityStatus == "available_local" {
			study.ViewerURL = "/ohif/"
		}

		studies = append(studies, study)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return studies, nil
}

func (a *App) listPhysicianResults(ctx context.Context, physicianID string, filters PhysicianSearchFilters) ([]PhysicianResult, error) {
	rows, err := a.db.QueryContext(ctx, `
		SELECT query_json
		FROM physician_recent_queries
		WHERE physician_id = $1::uuid
		ORDER BY searched_at DESC, id DESC
		LIMIT 10
	`, physicianID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := make([]PhysicianResult, 0)
	seen := make(map[string]struct{})

	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}

		var payload struct {
			PatientName string `json:"patient_name"`
			DateFrom    string `json:"date_from"`
			DateTo      string `json:"date_to"`
			Modalities  []string `json:"modalities"`
			Results     []struct {
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
			} `json:"results"`
		}

		if err := json.Unmarshal(raw, &payload); err != nil {
			return nil, fmt.Errorf("parse physician query_json: %w", err)
		}

		for _, item := range payload.Results {
			if _, ok := seen[item.StudyInstanceUID]; ok {
				continue
			}
			if filters.PatientName != "" && !strings.Contains(strings.ToUpper(item.PatientName), strings.ToUpper(filters.PatientName)) {
				continue
			}
			if filters.DateFrom != "" && item.StudyDate < filters.DateFrom {
				continue
			}
			if filters.DateTo != "" && item.StudyDate > filters.DateTo {
				continue
			}
			if filters.Modality != "" {
				match := false
				for _, modality := range item.Modalities {
					if strings.ToUpper(modality) == filters.Modality {
						match = true
						break
					}
				}
				if !match {
					continue
				}
			}

			result := PhysicianResult{
				StudyInstanceUID: item.StudyInstanceUID,
				PatientName:      item.PatientName,
				PatientID:        item.PatientID,
				StudyDate:        item.StudyDate,
				StudyDescription: item.StudyDescription,
				Modalities:       item.Modalities,
				Locations:        item.Locations,
				CacheStatus:      item.CacheStatus,
				RetrieveStatus:   item.RetrieveStatus,
				PartialFilter:    item.PartialFilter,
			}
			if item.RetrieveStatus == "done" || item.CacheStatus == "local_complete" {
				result.ViewerURL = "/ohif/"
			}

			results = append(results, result)
			seen[item.StudyInstanceUID] = struct{}{}
		}
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].StudyDate == results[j].StudyDate {
			return results[i].StudyInstanceUID < results[j].StudyInstanceUID
		}
		return results[i].StudyDate > results[j].StudyDate
	})

	return results, nil
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
