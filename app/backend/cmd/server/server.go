package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	_ "github.com/jackc/pgx/v5/stdlib"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	cfg := Config{
		AppEnv:               envOrDefault("APP_ENV", "dev"),
		ListenAddr:           envOrDefault("LISTEN_ADDR", ":8082"),
		PostgresDSN:          strings.TrimSpace(os.Getenv("POSTGRES_DSN")),
		OrthancURL:           strings.TrimRight(strings.TrimSpace(os.Getenv("ORTHANC_URL")), "/"),
		OrthancUser:          envOrDefault("ORTHANC_USERNAME", ""),
		OrthancPass:          envOrDefault("ORTHANC_PASSWORD", ""),
		OrthancInternalToken: strings.TrimSpace(os.Getenv("ORTHANC_INTERNAL_TOKEN")),
		ConfigPath:           envOrDefault("CONFIG_PATH", "/app/config/config.json"),
		MigrationsDir:        envOrDefault("MIGRATIONS_DIR", "/app/migrations"),
		LogLevel:             envOrDefault("LOG_LEVEL", "info"),
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
	prestacionLookup := PrestacionLookupSource(&NoopPrestacionLookupSource{})
	var legacyHIS *LegacyHISClient
	if externalConfig != nil {
		identitySource = buildPatientIdentitySource(*externalConfig, logger)
		professionalIdentitySource = buildProfessionalIdentitySource(*externalConfig, logger)
		if source, err := buildPrestacionLookupSource(*externalConfig, logger); err == nil {
			prestacionLookup = source
		} else {
			recordStartupIssue("prestaciones_lookup", err)
		}
		legacyHIS = buildLegacyHISClient(externalConfig.LegacyHIS, logger, recordStartupIssue)
	}

	app := &App{
		cfg: cfg,
		db:  db,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
		orthancSearchClient: &http.Client{
			Timeout: orthancCFindTimeout,
		},
		loginRateLimiter:           newInMemoryRateLimiter(),
		orthancModalities:          make(map[string]string),
		logger:                     logger,
		externalConfig:             externalConfig,
		configLoadedAt:             time.Now().UTC(),
		identitySource:             identitySource,
		professionalIdentitySource: professionalIdentitySource,
		prestacionLookup:           prestacionLookup,
		legacyHIS:                  legacyHIS,
		patientSearchQueue:         make(chan string, 32),
		retrieveQueue:              make(chan string, 32),
		scheduledRetrieveQueue:     make(chan string, 32),
		physicianAndesEnrichQueue:  make(chan physicianAndesEnrichJob, 64),
		retrieveEventSubscribers:   make(map[string]map[chan RetrieveJobEvent]struct{}),
		systemEventSubscribers:     make(map[chan SystemHealthEvent]struct{}),
		rbac:                       buildRBACPolicy(externalConfig),
	}

	if db != nil {
		app.usageRecorder = &PostgresUsageRecorder{db: db}
	}

	app.log("info", "prestacion_lookup_source_ready", map[string]any{
		"provider": prestacionLookup.ProviderName(),
		"mode":     prestacionLookup.Mode(),
		"his_provider": func() string {
			if externalConfig == nil {
				return ""
			}
			return strings.TrimSpace(externalConfig.HIS.Provider)
		}(),
		"prestaciones_enrichment_enabled": func() bool {
			if externalConfig == nil {
				return false
			}
			return externalConfig.HIS.PrestacionesEnrichmentEnabled
		}(),
		"his_token_present":    strings.TrimSpace(os.Getenv("HIS_TOKEN")) != "",
		"his_base_url_present": strings.TrimSpace(os.Getenv("HIS_BASE_URL")) != "",
	})

	app.startPatientSearchWorker()
	app.startRetrieveWorker()
	app.startScheduledRetrieveWorker()
	app.startPhysicianAndesEnrichWorker()
	app.startSystemHealthWatcher()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/livez", app.handleLivez)
	mux.HandleFunc("/api/health", app.handleHealth)
	mux.HandleFunc("/api/system/events", app.handleSystemEvents)
	mux.HandleFunc("/api/config", app.handleConfig(appliedMigrations))
	mux.HandleFunc("/api/runtime-config", app.handleRuntimeConfig)
	mux.HandleFunc("/api/patient/send-code", app.withBrowserOriginCheck(app.handlePatientSendCode))
	mux.HandleFunc("/api/patient/login", app.action(ActionPatientLogin, app.handlePatientLogin))
	mux.HandleFunc("/api/patient/logout", app.withBrowserOriginCheck(app.handlePatientLogout))
	mux.HandleFunc("/api/patient/search", app.withBrowserOriginCheck(app.handlePatientSearch))
	mux.HandleFunc("/api/patient/studies", app.handlePatientStudies)
	mux.HandleFunc("/api/patient/studies/", app.withBrowserOriginCheck(app.handlePatientStudyRoute))
	mux.HandleFunc("/api/patient/download", app.action(ActionStudyDownload, app.handlePatientDownload))
	mux.HandleFunc("/api/patient/report", app.handlePatientAndesReportDownload)
	mux.HandleFunc("/api/patient/retrieve", app.action(ActionStudyRetrieve, app.handlePatientRetrieve))
	mux.HandleFunc("/api/physician/login", app.action(ActionPhysicianLogin, app.handlePhysicianLogin))
	mux.HandleFunc("/api/physician/logout", app.withBrowserOriginCheck(app.handlePhysicianLogout))
	mux.HandleFunc("/api/retrieve/jobs/", app.handleRetrieveJobEvents)
	mux.HandleFunc("/api/physician/results", app.handlePhysicianResults)
	mux.HandleFunc("/api/physician/studies/", app.withBrowserOriginCheck(app.handlePhysicianStudyRoute))
	mux.HandleFunc("/api/physician/download", app.action(ActionStudyDownload, app.handlePhysicianDownload))
	mux.HandleFunc("/api/physician/report", app.handlePhysicianAndesReportDownload)
	mux.HandleFunc("/api/physician/retrieve", app.action(ActionStudyRetrieve, app.handlePhysicianRetrieve))
	mux.HandleFunc("/api/operator/usage/summary", app.action(ActionUsageRead, app.handleOperatorUsageSummary))
	mux.HandleFunc("/api/operator/usage/events", app.action(ActionUsageRead, app.handleOperatorUsageEvents))
	mux.HandleFunc("/api/operator/feedback", app.action(ActionFeedbackRead, app.handleFeedbackList))
	mux.HandleFunc("/api/feedback", app.action(ActionFeedbackCreate, app.handleFeedbackCreate))
	mux.HandleFunc("/api/orthanc-auth/tokens/", app.handleOrthancTokenCreate)
	mux.HandleFunc("/api/orthanc-auth/tokens/decode", app.handleOrthancTokenDecode)
	mux.HandleFunc("/api/orthanc-auth/tokens/validate", app.handleOrthancTokenValidation)
	mux.HandleFunc("/api/orthanc-auth/user/get-profile", app.handleOrthancUserProfile)
	mux.HandleFunc("/share", app.action(ActionShareLinkConsume, app.handleShareLanding))
	mux.HandleFunc("/viewer-access/", app.handleViewerAccess)

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
	if closer, ok := app.prestacionLookup.(patientIdentitySourceCloser); ok {
		defer func() {
			closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := closer.Close(closeCtx); err != nil {
				app.log("error", "prestacion_lookup_close_failed", map[string]any{"error": err.Error()})
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
		"degraded":            len(startupIssues) > 0,
		"startup_issue_count": len(startupIssues),
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
