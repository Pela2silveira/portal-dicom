package main

import (
	"net/url"
	"strings"
	"testing"
)

func TestLegacyCodigoToString(t *testing.T) {
	cases := []struct {
		name string
		raw  any
		want string
	}{
		{"nil", nil, ""},
		{"int64", int64(599456), "599456"},
		{"int", int(42), "42"},
		{"bytes with spaces", []byte("  599456  "), "599456"},
		{"string", "599456", "599456"},
		{"string trimmed", "  A123 ", "A123"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := legacyCodigoToString(tt.raw); got != tt.want {
				t.Errorf("legacyCodigoToString(%#v) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestLegacyHISLookupQueryUsesConfiguredDatabase(t *testing.T) {
	got := legacyHISLookupQuery(LegacyHISConfig{Database: "OtraBase"})
	if !strings.Contains(got, "[OtraBase].[dbo].[Historias_Clinicas]") {
		t.Errorf("query does not use configured database: %q", got)
	}
	if !strings.Contains(got, "HC_Documento = @dni") {
		t.Errorf("query missing parameterized filter: %q", got)
	}

	def := legacyHISLookupQuery(LegacyHISConfig{})
	if !strings.Contains(def, "[Hospital].[dbo].[Historias_Clinicas]") {
		t.Errorf("default database not applied: %q", def)
	}
}

func TestBuildLegacyHISDSN(t *testing.T) {
	dsn, err := buildLegacyHISDSN(LegacyHISConfig{
		Host:             "10.0.0.1",
		Port:             1433,
		Database:         "Hospital",
		User:             "aplicaciones_portalImg",
		Encrypt:          "disable",
		ConnectTimeoutMS: 5000,
	}, "s3cr3t")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("dsn is not a valid url: %v", err)
	}
	if u.Scheme != "sqlserver" {
		t.Errorf("scheme = %q, want sqlserver", u.Scheme)
	}
	if u.Host != "10.0.0.1:1433" {
		t.Errorf("host = %q, want 10.0.0.1:1433", u.Host)
	}
	user := u.User.Username()
	pass, _ := u.User.Password()
	if user != "aplicaciones_portalImg" || pass != "s3cr3t" {
		t.Errorf("credentials = %q/%q", user, pass)
	}
	q := u.Query()
	if q.Get("database") != "Hospital" {
		t.Errorf("database = %q, want Hospital", q.Get("database"))
	}
	if q.Get("encrypt") != "disable" {
		t.Errorf("encrypt = %q, want disable", q.Get("encrypt"))
	}
}

func TestBuildLegacyHISDSNValidation(t *testing.T) {
	if _, err := buildLegacyHISDSN(LegacyHISConfig{User: "u"}, "p"); err == nil {
		t.Error("expected error when host missing")
	}
	if _, err := buildLegacyHISDSN(LegacyHISConfig{Host: "h"}, "p"); err == nil {
		t.Error("expected error when user missing")
	}
	if _, err := buildLegacyHISDSN(LegacyHISConfig{Host: "h", User: "u"}, "  "); err == nil {
		t.Error("expected error when password empty")
	}
}
