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
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type Config struct {
	AppEnv      string
	ListenAddr  string
	PostgresDSN string
	OrthancURL  string
	OrthancUser string
	OrthancPass string
	ConfigPath  string
	LogLevel    string
}

type App struct {
	cfg        Config
	db         *sql.DB
	httpClient *http.Client
	logger     *log.Logger
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

func main() {
	cfg := Config{
		AppEnv:      envOrDefault("APP_ENV", "dev"),
		ListenAddr:  envOrDefault("LISTEN_ADDR", ":8081"),
		PostgresDSN: strings.TrimSpace(os.Getenv("POSTGRES_DSN")),
		OrthancURL:  strings.TrimRight(strings.TrimSpace(os.Getenv("ORTHANC_URL")), "/"),
		OrthancUser: envOrDefault("ORTHANC_USERNAME", ""),
		OrthancPass: envOrDefault("ORTHANC_PASSWORD", ""),
		ConfigPath:  envOrDefault("CONFIG_PATH", "/app/config/config.json"),
		LogLevel:    envOrDefault("LOG_LEVEL", "info"),
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

	app := &App{
		cfg: cfg,
		db:  db,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
		logger: logger,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", app.handleHealth)

	app.log("info", "server_starting", map[string]any{
		"listen_addr": cfg.ListenAddr,
		"app_env":     cfg.AppEnv,
		"log_level":   cfg.LogLevel,
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
	info, err := os.Stat(a.cfg.ConfigPath)
	if err != nil {
		a.log("error", "config_missing", map[string]any{"error": err.Error()})
		return false
	}

	return !info.IsDir()
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
