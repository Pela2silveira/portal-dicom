package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

type Config struct {
	AppEnv               string
	ListenAddr           string
	PostgresDSN          string
	OrthancURL           string
	OrthancUser          string
	OrthancPass          string
	OrthancInternalToken string
	ConfigPath           string
	MigrationsDir        string
	LogLevel             string
}

type ConfigResponse struct {
	AppEnv       string             `json:"app_env"`
	ConfigPath   string             `json:"config_path"`
	LoadedAt     string             `json:"loaded_at"`
	PACSNodes    []PACSNodeResponse `json:"pacs_nodes"`
	HIS          HISConfigResponse  `json:"his"`
	Portal       PortalConfig       `json:"portal"`
	Patient      PatientConfig      `json:"patient"`
	Professional ProfessionalConfig `json:"professional"`
	Cache        CacheConfig        `json:"cache"`
	Migrations   []string           `json:"migrations"`
}

type RuntimeConfigResponse struct {
	Portal  RuntimePortalConfigResponse  `json:"portal"`
	Patient RuntimePatientConfigResponse `json:"patient"`
}

type RuntimePortalConfigResponse struct {
	SessionTimeoutMinutes int  `json:"session_timeout_minutes"`
	ShowDemoRibbon        bool `json:"show_demo_ribbon"`
}

type ExternalConfig struct {
	PACSNodes     []PACSNodeConfig    `json:"pacs_nodes"`
	HIS           HISConfig           `json:"his"`
	Portal        PortalConfig        `json:"portal"`
	Patient       PatientConfig       `json:"patient"`
	Professional  ProfessionalConfig  `json:"professional"`
	Cache         CacheConfig         `json:"cache"`
	RBAC          RBACConfig          `json:"rbac"`
	Observability ObservabilityConfig `json:"observability"`
}

// RBACConfig holds the (optional) role-based access control policy. Roles are
// assigned via simple config lists (no IdP/LDAP role integration): the only
// supported elevation today is granting the share permission to specific
// physicians via physician_sharers. Anything omitted falls back to the in-code
// defaults declared in rbac.go.
type RBACConfig struct {
	Roles                map[string][]string `json:"roles"`
	PhysicianSharers     []string            `json:"physician_sharers"`
	PhysicianDefaultRole string              `json:"physician_default_role"`
	// Operators are professionals (by username/DNI) granted the operator role,
	// which unlocks the read-only usage metrics and audited-actions view.
	Operators []string `json:"operators"`
}

// ObservabilityConfig toggles audit logging and usage metrics. When *Enabled is
// nil the action catalog defaults apply; an explicit allowlist (AuditActions /
// MeteredActions) overrides the per-action defaults entirely.
type ObservabilityConfig struct {
	AuditEnabled   *bool    `json:"audit_enabled"`
	MetricsEnabled *bool    `json:"metrics_enabled"`
	AuditActions   []string `json:"audit_actions"`
	MeteredActions []string `json:"metered_actions"`
}

type PACSNodeConfig struct {
	ID                  string                     `json:"id"`
	Name                string                     `json:"name"`
	AndesOrganizationID string                     `json:"andes_organization_id,omitempty"`
	HIS                 bool                       `json:"his,omitempty"`
	TipoPrestacion      []PACSTipoPrestacionConfig `json:"tipoPrestacion,omitempty"`
	Protocol            string                     `json:"protocol"`
	Priority            int                        `json:"priority"`
	AET                 string                     `json:"aet"`
	DICOMHost           string                     `json:"dicom_host"`
	DICOMPort           int                        `json:"dicom_port"`
	DICOMwebBaseURL     string                     `json:"dicomweb_base_url"`
	SupportsCMove       bool                       `json:"supports_cmove"`
	SupportsCGet        bool                       `json:"supports_cget"`
	PatientIDSource     string                     `json:"patient_id_source,omitempty"`
	Auth                PACSAuthConfig             `json:"auth"`
	Search              PACSNodeSearchConfig       `json:"search"`
	Retrieve            PACSNodeRetrieveConfig     `json:"retrieve"`
	Health              PACSNodeHealthConfig       `json:"health"`
}

type PACSAuthConfig struct {
	Type            string `json:"type"`
	TokenURL        string `json:"token_url"`
	ClientIDEnv     string `json:"client_id_env"`
	ClientSecretEnv string `json:"client_secret_env"`
}

type HISConfig struct {
	Provider                      string   `json:"provider"`
	Enabled                       bool     `json:"enabled"`
	BaseURL                       string   `json:"base_url"`
	AuthType                      string   `json:"auth_type"`
	DocumentLookupPath            string   `json:"document_lookup_path"`
	PrestacionesEnrichmentEnabled bool     `json:"prestaciones_enrichment_enabled"`
	PrestacionesProvider          string   `json:"prestaciones_provider,omitempty"`
	AndesUIDPrefixes              []string `json:"andes_uid_prefixes,omitempty"`
	AndesRESTConcurrency          int      `json:"andes_rest_concurrency,omitempty"`
	AndesRESTRequestTimeoutMS     int      `json:"andes_rest_request_timeout_ms,omitempty"`
}

type ProfessionalConfig struct {
	FakeAuth            bool     `json:"fake_auth"`
	InitialCachePeriod  string   `json:"initial_cache_period"`
	WeeklyDownloadLimit int      `json:"weekly_download_limit"`
	LicenseExceptions   []string `json:"license_exceptions"`
}

type PACSNodeResolvedConfig struct {
	ID                  string
	Name                string
	AndesOrganizationID string
	HIS                 bool
	TipoPrestacion      []PACSTipoPrestacionConfig
	Protocol            string
	Priority            int
	AET                 string
	DICOMHost           string
	DICOMPort           int
	DICOMwebBaseURL     string
	SupportsCMove       bool
	SupportsCGet        bool
	PatientIDSource     string
	Auth                PACSAuthConfig
	SearchMode          string
	RetrieveMode        string
	HealthMode          string
	HealthCallingAET    string
}

type HISConfigResponse struct {
	Provider                      string   `json:"provider"`
	Enabled                       bool     `json:"enabled"`
	BaseURL                       string   `json:"base_url"`
	AuthType                      string   `json:"auth_type"`
	DocumentLookupPath            string   `json:"document_lookup_path"`
	PrestacionesEnrichmentEnabled bool     `json:"prestaciones_enrichment_enabled"`
	PrestacionesProvider          string   `json:"prestaciones_provider"`
	AndesUIDPrefixes              []string `json:"andes_uid_prefixes"`
	AndesRESTConcurrency          int      `json:"andes_rest_concurrency"`
	AndesRESTRequestTimeoutMS     int      `json:"andes_rest_request_timeout_ms"`
	HISTokenPresent               bool     `json:"his_token_present"`
	HISBaseURLPresent             bool     `json:"his_base_url_present"`
}

type PortalConfig struct {
	SessionTimeoutMinutes            int      `json:"session_timeout_minutes"`
	ShowDemoRibbon                   bool     `json:"show_demo_ribbon"`
	PreviewImageLimit                int      `json:"preview_image_limit"`
	RetrieveProgressPollSeconds      int      `json:"retrieve_progress_poll_seconds"`
	RetrieveWorkerConcurrency        int      `json:"retrieve_worker_concurrency"`
	ScheduledRetrieveEnabled         bool     `json:"scheduled_retrieve_enabled"`
	ScheduledRetrieveIntervalMinutes int      `json:"scheduled_retrieve_interval_minutes"`
	ScheduledRetrieveMaxStudyAgeDays int      `json:"scheduled_retrieve_max_study_age_days"`
	ScheduledRetrieveBatchSize       int      `json:"scheduled_retrieve_batch_size"`
	RetrieveBlockedModalities        []string `json:"retrieve_blocked_modalities"`
	RetrieveMaxAttempts              int      `json:"retrieve_max_attempts"`
	RetrieveRetryBackoffSeconds      int      `json:"retrieve_retry_backoff_seconds"`
	RetrieveTimeoutMinutes           int      `json:"retrieve_timeout_minutes"`
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
				Provider:                      a.externalConfig.HIS.Provider,
				Enabled:                       a.externalConfig.HIS.Enabled,
				BaseURL:                       a.externalConfig.HIS.BaseURL,
				AuthType:                      a.externalConfig.HIS.AuthType,
				DocumentLookupPath:            a.externalConfig.HIS.DocumentLookupPath,
				PrestacionesEnrichmentEnabled: a.externalConfig.HIS.PrestacionesEnrichmentEnabled,
				PrestacionesProvider:          a.externalConfig.HIS.ResolvedPrestacionesProvider(),
				AndesUIDPrefixes:              a.externalConfig.HIS.ResolvedAndesUIDPrefixes(),
				AndesRESTConcurrency:          a.externalConfig.HIS.ResolvedAndesRESTConcurrency(),
				AndesRESTRequestTimeoutMS:     int(a.externalConfig.HIS.ResolvedAndesRESTRequestTimeout() / time.Millisecond),
				HISTokenPresent:               strings.TrimSpace(os.Getenv("HIS_TOKEN")) != "",
				HISBaseURLPresent:             strings.TrimSpace(os.Getenv("HIS_BASE_URL")) != "",
			},
			Portal:       a.externalConfig.Portal,
			Patient:      a.externalConfig.Patient,
			Professional: a.externalConfig.Professional,
			Cache:        a.externalConfig.Cache,
			Migrations:   appliedMigrations,
		}

		for _, node := range a.externalConfig.PACSNodes {
			resolved := node.Resolved()
			authResponse := PACSAuthResponse{
				Type:                resolved.Auth.Type,
				TokenURL:            resolved.Auth.TokenURL,
				ClientIDEnv:         resolved.Auth.ClientIDEnv,
				ClientSecretEnv:     resolved.Auth.ClientSecretEnv,
				ClientIDPresent:     strings.TrimSpace(os.Getenv(resolved.Auth.ClientIDEnv)) != "",
				ClientSecretPresent: strings.TrimSpace(os.Getenv(resolved.Auth.ClientSecretEnv)) != "",
			}
			resp.PACSNodes = append(resp.PACSNodes, PACSNodeResponse{
				ID:                  resolved.ID,
				Name:                resolved.Name,
				AndesOrganizationID: resolved.AndesOrganizationID,
				HIS:                 resolved.HIS,
				TipoPrestacion:      resolved.TipoPrestacion,
				Protocol:            resolved.Protocol,
				Priority:            resolved.Priority,
				AET:                 resolved.AET,
				DICOMHost:           resolved.DICOMHost,
				DICOMPort:           resolved.DICOMPort,
				DICOMwebBaseURL:     resolved.DICOMwebBaseURL,
				SupportsCMove:       resolved.SupportsCMove,
				SupportsCGet:        resolved.SupportsCGet,
				Auth:                authResponse,
				Search: PACSNodeSearchResponse{
					Mode:            resolved.SearchMode,
					DICOMwebBaseURL: resolved.DICOMwebBaseURL,
					Auth:            authResponse,
				},
				Retrieve: PACSNodeRetrieveResponse{
					Mode:          resolved.RetrieveMode,
					AET:           resolved.AET,
					DICOMHost:     resolved.DICOMHost,
					DICOMPort:     resolved.DICOMPort,
					SupportsCMove: resolved.SupportsCMove,
					SupportsCGet:  resolved.SupportsCGet,
				},
				Health: PACSNodeHealthResponse{
					Mode:       resolved.HealthMode,
					CallingAET: resolved.HealthCallingAET,
				},
			})
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}

func (a *App) handleRuntimeConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if a.externalConfig == nil {
		http.Error(w, "config not loaded", http.StatusServiceUnavailable)
		return
	}

	writeJSON(w, http.StatusOK, RuntimeConfigResponse{
		Portal: RuntimePortalConfigResponse{
			SessionTimeoutMinutes: a.externalConfig.Portal.SessionTimeoutMinutes,
			ShowDemoRibbon:        a.externalConfig.Portal.ShowDemoRibbon,
		},
		Patient: RuntimePatientConfigResponse{
			AuthMode: a.externalConfig.Patient.ResolvedAuthMode(),
		},
	})
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

func (n PACSNodeConfig) Resolved() PACSNodeResolvedConfig {
	resolved := PACSNodeResolvedConfig{
		ID:                  n.ID,
		Name:                n.Name,
		AndesOrganizationID: strings.TrimSpace(n.AndesOrganizationID),
		HIS:                 n.HIS,
		TipoPrestacion:      append([]PACSTipoPrestacionConfig(nil), n.TipoPrestacion...),
		Protocol:            strings.TrimSpace(n.Protocol),
		Priority:            n.Priority,
		AET:                 strings.TrimSpace(n.AET),
		DICOMHost:           strings.TrimSpace(n.DICOMHost),
		DICOMPort:           n.DICOMPort,
		DICOMwebBaseURL:     strings.TrimSpace(n.DICOMwebBaseURL),
		SupportsCMove:       n.SupportsCMove,
		SupportsCGet:        n.SupportsCGet,
		PatientIDSource:     normalizePatientIDSource(n.PatientIDSource),
		Auth:                n.Auth,
		SearchMode:          strings.TrimSpace(n.Protocol),
		HealthMode:          "http",
	}

	if strings.TrimSpace(n.Search.Mode) != "" {
		resolved.SearchMode = strings.TrimSpace(n.Search.Mode)
	}
	if strings.TrimSpace(n.Search.DICOMwebBaseURL) != "" {
		resolved.DICOMwebBaseURL = strings.TrimSpace(n.Search.DICOMwebBaseURL)
	}
	if strings.TrimSpace(n.Search.Auth.Type) != "" {
		resolved.Auth = n.Search.Auth
	}

	if strings.TrimSpace(n.Retrieve.Mode) != "" {
		resolved.RetrieveMode = strings.TrimSpace(n.Retrieve.Mode)
	}
	if strings.TrimSpace(n.Retrieve.AET) != "" {
		resolved.AET = strings.TrimSpace(n.Retrieve.AET)
	}
	if strings.TrimSpace(n.Retrieve.DICOMHost) != "" {
		resolved.DICOMHost = strings.TrimSpace(n.Retrieve.DICOMHost)
	}
	if n.Retrieve.DICOMPort != 0 {
		resolved.DICOMPort = n.Retrieve.DICOMPort
	}
	if n.Retrieve.SupportsCMove {
		resolved.SupportsCMove = true
	}
	if n.Retrieve.SupportsCGet {
		resolved.SupportsCGet = true
	}

	if resolved.RetrieveMode == "" {
		if resolved.SupportsCMove {
			resolved.RetrieveMode = "c_move"
		} else if resolved.SupportsCGet {
			resolved.RetrieveMode = "c_get"
		}
	}

	if strings.TrimSpace(n.Health.Mode) != "" {
		resolved.HealthMode = strings.TrimSpace(n.Health.Mode)
	} else if resolved.SearchMode == "c_find" || resolved.RetrieveMode == "c_move" || resolved.RetrieveMode == "c_get" {
		resolved.HealthMode = "mixed"
	}
	if strings.TrimSpace(n.Health.CallingAET) != "" {
		resolved.HealthCallingAET = strings.TrimSpace(n.Health.CallingAET)
	}

	if resolved.Protocol == "" {
		switch {
		case resolved.SearchMode == "qido_rs" && (resolved.RetrieveMode == "c_move" || resolved.RetrieveMode == "c_get"):
			resolved.Protocol = "hybrid"
		case resolved.SearchMode == "qido_rs":
			resolved.Protocol = "dicomweb"
		case resolved.SearchMode == "c_find":
			resolved.Protocol = "dimse"
		default:
			resolved.Protocol = "hybrid"
		}
	}

	return resolved
}

func loadExternalConfig(path string) (*ExternalConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	cfg := ExternalConfig{
		Portal: PortalConfig{
			SessionTimeoutMinutes:            10,
			ShowDemoRibbon:                   false,
			PreviewImageLimit:                5,
			RetrieveProgressPollSeconds:      5,
			RetrieveWorkerConcurrency:        2,
			ScheduledRetrieveEnabled:         false,
			ScheduledRetrieveIntervalMinutes: 60,
			ScheduledRetrieveMaxStudyAgeDays: 7,
			ScheduledRetrieveBatchSize:       5,
			RetrieveMaxAttempts:              3,
			RetrieveRetryBackoffSeconds:      10,
			RetrieveTimeoutMinutes:           30,
		},
		Patient: PatientConfig{
			AuthMode: PatientAuthModeFakeAuth,
			FakeAuth: true,
		},
		Professional: ProfessionalConfig{
			FakeAuth:            true,
			InitialCachePeriod:  "current_week",
			WeeklyDownloadLimit: 100,
		},
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parse config json: %w", err)
	}

	return &cfg, nil
}

func (a *App) getConfiguredNode(nodeID string) (PACSNodeConfig, error) {
	for _, node := range a.externalConfig.PACSNodes {
		if node.ID == nodeID {
			return node, nil
		}
	}
	return PACSNodeConfig{}, fmt.Errorf("configured PACS node %q not found", nodeID)
}

func (a *App) configuredPACSNodeByID(nodeID string) (PACSNodeConfig, bool) {
	for _, node := range a.externalConfig.PACSNodes {
		if strings.EqualFold(strings.TrimSpace(node.ID), strings.TrimSpace(nodeID)) {
			return node, true
		}
	}
	return PACSNodeConfig{}, false
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

func (a *App) resolveConfiguredNodeIDForStudy(sourceNodeID string, locations []string) string {
	if strings.TrimSpace(sourceNodeID) != "" {
		return strings.TrimSpace(sourceNodeID)
	}
	for _, location := range locations {
		for _, node := range a.externalConfig.PACSNodes {
			if strings.EqualFold(strings.TrimSpace(node.Name), strings.TrimSpace(location)) || strings.EqualFold(strings.TrimSpace(node.ID), strings.TrimSpace(location)) {
				return node.ID
			}
		}
	}
	return ""
}

func validateExternalConfig(cfg ExternalConfig) error {
	if cfg.Portal.SessionTimeoutMinutes <= 0 {
		return errors.New("portal.session_timeout_minutes must be greater than 0")
	}
	if cfg.Portal.RetrieveProgressPollSeconds <= 0 {
		return errors.New("portal.retrieve_progress_poll_seconds must be greater than 0")
	}
	if cfg.Portal.RetrieveWorkerConcurrency <= 0 {
		return errors.New("portal.retrieve_worker_concurrency must be greater than 0")
	}
	if cfg.Portal.ScheduledRetrieveIntervalMinutes <= 0 {
		return errors.New("portal.scheduled_retrieve_interval_minutes must be greater than 0")
	}
	if cfg.Portal.ScheduledRetrieveMaxStudyAgeDays <= 0 {
		return errors.New("portal.scheduled_retrieve_max_study_age_days must be greater than 0")
	}
	if cfg.Portal.ScheduledRetrieveBatchSize <= 0 {
		return errors.New("portal.scheduled_retrieve_batch_size must be greater than 0")
	}

	switch cfg.Patient.ResolvedAuthMode() {
	case PatientAuthModeMail, PatientAuthModeFakeAuth:
	case PatientAuthModeMasterKey:
		if strings.TrimSpace(patientMasterKey()) == "" {
			return errors.New(`PATIENT_MASTER_KEY env var is required when patient.auth_mode = "master_key"`)
		}
	default:
		return fmt.Errorf("invalid patient auth mode %q", cfg.Patient.AuthMode)
	}

	if len(cfg.PACSNodes) == 0 {
		return errors.New("config must include at least one PACS node")
	}

	for _, node := range cfg.PACSNodes {
		resolved := node.Resolved()
		if strings.TrimSpace(node.ID) == "" {
			return errors.New("pacs node id is required")
		}
		if strings.TrimSpace(node.Name) == "" {
			return fmt.Errorf("pacs node %q name is required", node.ID)
		}
		if strings.TrimSpace(resolved.Protocol) == "" {
			return fmt.Errorf("pacs node %q protocol is required", node.ID)
		}
		if resolved.SearchMode == "qido_rs" && strings.TrimSpace(resolved.DICOMwebBaseURL) == "" {
			return fmt.Errorf("pacs node %q dicomweb_base_url is required for qido_rs search", node.ID)
		}
		if (resolved.RetrieveMode == "c_move" || resolved.RetrieveMode == "c_get") && (strings.TrimSpace(resolved.AET) == "" || strings.TrimSpace(resolved.DICOMHost) == "" || resolved.DICOMPort == 0) {
			return fmt.Errorf("pacs node %q dimse retrieve requires aet, dicom_host and dicom_port", node.ID)
		}

		if resolved.Auth.Type == "keycloak_client_credentials" {
			if strings.TrimSpace(resolved.Auth.TokenURL) == "" {
				return fmt.Errorf("pacs node %q token_url is required", node.ID)
			}
			if strings.TrimSpace(resolved.Auth.ClientIDEnv) == "" || strings.TrimSpace(resolved.Auth.ClientSecretEnv) == "" {
				return fmt.Errorf("pacs node %q client env refs are required", node.ID)
			}
			if strings.TrimSpace(os.Getenv(resolved.Auth.ClientIDEnv)) == "" {
				return fmt.Errorf("pacs node %q missing env value for %s", node.ID, resolved.Auth.ClientIDEnv)
			}
			if strings.TrimSpace(os.Getenv(resolved.Auth.ClientSecretEnv)) == "" {
				return fmt.Errorf("pacs node %q missing env value for %s", node.ID, resolved.Auth.ClientSecretEnv)
			}
		}
	}

	return nil
}

func persistExternalConfig(ctx context.Context, db *sql.DB, cfg ExternalConfig) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, node := range cfg.PACSNodes {
		resolved := node.Resolved()
		authJSON, err := json.Marshal(map[string]any{
			"type":              resolved.Auth.Type,
			"token_url":         resolved.Auth.TokenURL,
			"client_id_env":     resolved.Auth.ClientIDEnv,
			"client_secret_env": resolved.Auth.ClientSecretEnv,
			"search": map[string]any{
				"mode":              resolved.SearchMode,
				"dicomweb_base_url": resolved.DICOMwebBaseURL,
			},
			"retrieve": map[string]any{
				"mode":           resolved.RetrieveMode,
				"aet":            resolved.AET,
				"dicom_host":     resolved.DICOMHost,
				"dicom_port":     resolved.DICOMPort,
				"supports_cmove": resolved.SupportsCMove,
				"supports_cget":  resolved.SupportsCGet,
			},
			"health": map[string]any{
				"mode": resolved.HealthMode,
			},
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
			resolved.Name,
			resolved.Protocol,
			resolved.Priority,
			resolved.AET,
			resolved.DICOMHost,
			resolved.DICOMPort,
			resolved.DICOMwebBaseURL,
			resolved.SupportsCMove,
			resolved.SupportsCGet,
			resolved.Auth.Type,
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
