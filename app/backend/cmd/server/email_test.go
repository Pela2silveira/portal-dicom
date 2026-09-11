package main

import (
	"strings"
	"testing"
)

func TestBuildMailMessageHasDeliverabilityHeaders(t *testing.T) {
	cfg := smtpConfig{From: "no-reply@salud.example.gob.ar", FromName: "Portal de Imágenes"}
	msg := buildMailMessage(cfg, "paciente@example.com", "Codigo de acceso", "Su codigo es 123456",
		"Mon, 02 Jan 2006 15:04:05 -0300", "<123.abc@salud.example.gob.ar>")

	for _, want := range []string{
		"Date: Mon, 02 Jan 2006 15:04:05 -0300",
		"Message-ID: <123.abc@salud.example.gob.ar>",
		"To: paciente@example.com",
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message missing header %q\n---\n%s", want, msg)
		}
	}

	// CRLF line endings and a blank line separating headers from body.
	if !strings.Contains(msg, "\r\n\r\nSu codigo es 123456") {
		t.Errorf("message body not separated from headers by CRLF blank line:\n%s", msg)
	}
}

func TestFromHeaderEncodesDisplayName(t *testing.T) {
	// Non-ASCII display name must be RFC 2047 encoded and keep the bare address.
	got := smtpConfig{From: "no-reply@x.gob.ar", FromName: "Imágenes"}.fromHeader()
	if !strings.Contains(got, "<no-reply@x.gob.ar>") {
		t.Errorf("fromHeader missing angle-bracket address: %q", got)
	}
	if strings.Contains(got, "Imágenes") {
		t.Errorf("fromHeader must encode non-ASCII name, got raw: %q", got)
	}
	if !strings.Contains(got, "=?") {
		t.Errorf("fromHeader must contain an RFC 2047 encoded-word: %q", got)
	}

	// No display name -> bare address, unchanged.
	if bare := (smtpConfig{From: "no-reply@x.gob.ar"}).fromHeader(); bare != "no-reply@x.gob.ar" {
		t.Errorf("fromHeader without name = %q, want bare address", bare)
	}
}

func TestHeloNamePrecedence(t *testing.T) {
	if got := (smtpConfig{HELO: "mail.gob.ar", From: "a@other.ar"}).heloName(); got != "mail.gob.ar" {
		t.Errorf("explicit SMTP_HELO should win, got %q", got)
	}
	if got := (smtpConfig{From: "a@salud.gob.ar"}).heloName(); got != "salud.gob.ar" {
		t.Errorf("heloName should fall back to From domain, got %q", got)
	}
	// Never announce "localhost" when a From domain is available.
	if got := (smtpConfig{From: "a@salud.gob.ar"}).heloName(); got == "localhost" {
		t.Errorf("heloName must not be localhost when From domain is present")
	}
}

func TestFromDomain(t *testing.T) {
	cases := map[string]string{
		"a@b.gob.ar":   "b.gob.ar",
		"no-domain":    "",
		"trailing@":    "",
		"a@b@c.gob.ar": "c.gob.ar",
	}
	for in, want := range cases {
		if got := (smtpConfig{From: in}).fromDomain(); got != want {
			t.Errorf("fromDomain(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGenerateMessageIDFormat(t *testing.T) {
	id, err := generateMessageID("salud.gob.ar")
	if err != nil {
		t.Fatalf("generateMessageID error: %v", err)
	}
	if !strings.HasPrefix(id, "<") || !strings.HasSuffix(id, ">") {
		t.Errorf("message-id must be angle-bracketed: %q", id)
	}
	if !strings.HasSuffix(id, "@salud.gob.ar>") {
		t.Errorf("message-id must be anchored to the sender domain: %q", id)
	}

	// Empty domain must still yield a valid, non-empty domain part.
	id2, err := generateMessageID("")
	if err != nil {
		t.Fatalf("generateMessageID(\"\") error: %v", err)
	}
	if !strings.Contains(id2, "@") || strings.HasSuffix(id2, "@>") {
		t.Errorf("message-id with empty domain must fall back to a domain: %q", id2)
	}

	// Two calls should not collide.
	if id3, _ := generateMessageID("salud.gob.ar"); id3 == id {
		t.Errorf("message-id should be unique across calls")
	}
}
