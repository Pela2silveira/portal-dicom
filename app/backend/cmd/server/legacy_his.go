package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	_ "github.com/microsoft/go-mssqldb"
)

const (
	legacyHISDefaultPort           = 1433
	legacyHISDefaultConnectTimeout = 5 * time.Second
	legacyHISDefaultQueryTimeout   = 5 * time.Second
	legacyHISDefaultDatabase       = "Hospital"
	legacyHISDefaultEncrypt        = "disable"
	legacyHISDefaultPasswordEnv    = "LEGACY_HIS_PASSWORD"
)

// LegacyHISClient resolves a DNI into the internal patient code (Historias_Clinicas.Codigo)
// used by some PACS nodes as the DICOM PatientID. It wraps a pooled Microsoft SQL
// Server connection.
type LegacyHISClient struct {
	db           *sql.DB
	query        string
	queryTimeout time.Duration
	logger       *log.Logger
}

// buildLegacyHISDSN builds a sqlserver:// DSN from the config plus the password.
// The password is passed separately so it never has to live in the JSON config.
func buildLegacyHISDSN(cfg LegacyHISConfig, password string) (string, error) {
	host := strings.TrimSpace(cfg.Host)
	if host == "" {
		return "", errors.New("legacy_his: host is required")
	}
	user := strings.TrimSpace(cfg.User)
	if user == "" {
		return "", errors.New("legacy_his: user is required")
	}
	if strings.TrimSpace(password) == "" {
		return "", errors.New("legacy_his: password is empty")
	}

	port := cfg.Port
	if port <= 0 {
		port = legacyHISDefaultPort
	}
	encrypt := strings.TrimSpace(cfg.Encrypt)
	if encrypt == "" {
		encrypt = legacyHISDefaultEncrypt
	}
	connectTimeout := legacyHISDefaultConnectTimeout
	if cfg.ConnectTimeoutMS > 0 {
		connectTimeout = time.Duration(cfg.ConnectTimeoutMS) * time.Millisecond
	}

	u := &url.URL{
		Scheme: "sqlserver",
		User:   url.UserPassword(user, password),
		Host:   fmt.Sprintf("%s:%d", host, port),
	}
	q := url.Values{}
	q.Set("database", legacyHISDatabase(cfg))
	q.Set("encrypt", encrypt)
	q.Set("dial timeout", strconv.Itoa(int(connectTimeout.Seconds())))
	q.Set("connection timeout", strconv.Itoa(int(connectTimeout.Seconds())))
	q.Set("app name", "portal-img")
	u.RawQuery = q.Encode()

	return u.String(), nil
}

func legacyHISDatabase(cfg LegacyHISConfig) string {
	if db := strings.TrimSpace(cfg.Database); db != "" {
		return db
	}
	return legacyHISDefaultDatabase
}

// legacyHISLookupQuery returns the parameterized query for the configured
// database. Schema, table and columns are fixed by the legacy HIS layout.
func legacyHISLookupQuery(cfg LegacyHISConfig) string {
	return fmt.Sprintf(
		"SELECT TOP 1 [Codigo] FROM [%s].[dbo].[Historias_Clinicas] WHERE HC_Documento = @dni",
		legacyHISDatabase(cfg),
	)
}

// buildLegacyHISClient constructs the legacy HIS client from config when
// enabled. Startup is resilient: a construction error is recorded but returns a
// nil client (DNI searches for legacy_his nodes simply won't map). Connectivity
// is verified best-effort so a temporarily-down MSSQL does not block boot.
func buildLegacyHISClient(cfg LegacyHISConfig, logger *log.Logger, recordStartupIssue func(string, error)) *LegacyHISClient {
	if !cfg.Enabled {
		return nil
	}

	passwordEnv := strings.TrimSpace(cfg.PasswordEnv)
	if passwordEnv == "" {
		passwordEnv = legacyHISDefaultPasswordEnv
	}
	password := strings.TrimSpace(os.Getenv(passwordEnv))

	client, err := newLegacyHISClient(cfg, password, logger)
	if err != nil {
		if recordStartupIssue != nil {
			recordStartupIssue("legacy_his", err)
		}
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), client.queryTimeout)
	defer cancel()
	if err := client.Ping(ctx); err != nil {
		if recordStartupIssue != nil {
			recordStartupIssue("legacy_his_ping", err)
		}
		if logger != nil {
			logger.Printf("legacy_his: initial ping failed (will retry on demand): %v", err)
		}
	} else if logger != nil {
		logger.Printf("legacy_his: connected to %s", cfg.Host)
	}

	return client
}

func newLegacyHISClient(cfg LegacyHISConfig, password string, logger *log.Logger) (*LegacyHISClient, error) {
	dsn, err := buildLegacyHISDSN(cfg, password)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlserver", dsn)
	if err != nil {
		return nil, fmt.Errorf("legacy_his: open: %w", err)
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	db.SetConnMaxIdleTime(5 * time.Minute)
	db.SetConnMaxLifetime(30 * time.Minute)

	queryTimeout := legacyHISDefaultQueryTimeout
	if cfg.QueryTimeoutMS > 0 {
		queryTimeout = time.Duration(cfg.QueryTimeoutMS) * time.Millisecond
	}

	return &LegacyHISClient{
		db:           db,
		query:        legacyHISLookupQuery(cfg),
		queryTimeout: queryTimeout,
		logger:       logger,
	}, nil
}

// Ping verifies connectivity. It is best-effort at startup: a failure is logged
// but does not prevent the client from being used later (the legacy HIS may come
// back up without a restart).
func (c *LegacyHISClient) Ping(ctx context.Context) error {
	if c == nil || c.db == nil {
		return errors.New("legacy_his: client not initialized")
	}
	ctx, cancel := context.WithTimeout(ctx, c.queryTimeout)
	defer cancel()
	return c.db.PingContext(ctx)
}

// LookupCodigoByDocument returns the Historias_Clinicas.Codigo for the given DNI,
// or an empty string when no row matches.
func (c *LegacyHISClient) LookupCodigoByDocument(ctx context.Context, documentNumber string) (string, error) {
	if c == nil || c.db == nil {
		return "", errors.New("legacy_his: client not initialized")
	}
	documentNumber = strings.TrimSpace(documentNumber)
	if documentNumber == "" {
		return "", nil
	}

	ctx, cancel := context.WithTimeout(ctx, c.queryTimeout)
	defer cancel()

	var raw any
	err := c.db.QueryRowContext(ctx, c.query, sql.Named("dni", documentNumber)).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("legacy_his: lookup codigo: %w", err)
	}
	return legacyCodigoToString(raw), nil
}

func (c *LegacyHISClient) Close() error {
	if c == nil || c.db == nil {
		return nil
	}
	return c.db.Close()
}

// legacyCodigoToString normalizes the scanned Codigo value (which the driver may
// return as int64, []byte or string depending on the column type) into a trimmed
// string.
func legacyCodigoToString(raw any) string {
	switch v := raw.(type) {
	case nil:
		return ""
	case int64:
		return strconv.FormatInt(v, 10)
	case int:
		return strconv.Itoa(v)
	case []byte:
		return strings.TrimSpace(string(v))
	case string:
		return strings.TrimSpace(v)
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	}
}
