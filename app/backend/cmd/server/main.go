package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
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
			Timeout: 5 * time.Second,
		},
		logger:         logger,
		externalConfig: externalConfig,
		configLoadedAt: time.Now().UTC(),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", app.handleHealth)
	mux.HandleFunc("/api/config", app.handleConfig(appliedMigrations))

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
