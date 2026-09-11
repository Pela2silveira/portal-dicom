package main

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/smtp"
	"os"
	"strings"
	"time"
	"unicode/utf8"
)

type smtpConfig struct {
	Host     string
	Port     string
	Secure   bool
	AuthUser string
	AuthPass string
	From     string
	FromName string
	HELO     string
}

// fromDomain returns the domain part of the envelope/from address, or "" when
// the address has no usable domain.
func (cfg smtpConfig) fromDomain() string {
	if at := strings.LastIndex(cfg.From, "@"); at >= 0 && at < len(cfg.From)-1 {
		return cfg.From[at+1:]
	}
	return ""
}

// heloName resolves the hostname announced in the SMTP HELO/EHLO. A bogus name
// (the net/smtp default is "localhost") is a strong spam/reject signal, so we
// prefer an explicit SMTP_HELO, then the From domain, then the OS hostname.
func (cfg smtpConfig) heloName() string {
	if cfg.HELO != "" {
		return cfg.HELO
	}
	if d := cfg.fromDomain(); d != "" {
		return d
	}
	if h, err := os.Hostname(); err == nil && strings.TrimSpace(h) != "" {
		return strings.TrimSpace(h)
	}
	return "localhost"
}

// fromHeader builds the RFC 5322 "From" header value, RFC 2047-encoding the
// optional display name so non-ASCII names survive.
func (cfg smtpConfig) fromHeader() string {
	if cfg.FromName == "" {
		return cfg.From
	}
	return mime.QEncoding.Encode("UTF-8", cfg.FromName) + " <" + cfg.From + ">"
}

// generateMessageID returns a globally-unique RFC 5322 Message-ID anchored to
// the sender domain. Missing Message-ID is heavily penalized by spam filters.
func generateMessageID(domain string) (string, error) {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		domain = "localhost"
	}
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return fmt.Sprintf("<%d.%s@%s>", time.Now().UnixNano(), base64.RawURLEncoding.EncodeToString(b[:]), domain), nil
}

// buildMailMessage assembles the RFC 5322 message. Kept pure (date and
// message-id are injected) so the header set can be unit-tested.
func buildMailMessage(cfg smtpConfig, recipient, subject, body, date, messageID string) string {
	return strings.Join([]string{
		"From: " + cfg.fromHeader(),
		"To: " + recipient,
		"Subject: " + mime.QEncoding.Encode("UTF-8", subject),
		"Date: " + date,
		"Message-ID: " + messageID,
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"Content-Transfer-Encoding: 8bit",
		"",
		body,
		"",
	}, "\r\n")
}

func generateNumericCode(length int) (string, error) {
	if length <= 0 {
		return "", errors.New("invalid code length")
	}
	digits := make([]byte, length)
	for i := 0; i < length; i++ {
		var b [1]byte
		if _, err := rand.Read(b[:]); err != nil {
			return "", err
		}
		digits[i] = '0' + (b[0] % 10)
	}
	return string(digits), nil
}

func smtpConfigFromEnv() (smtpConfig, error) {
	cfg := smtpConfig{
		Host:     strings.TrimSpace(os.Getenv("SMTP_HOST")),
		Port:     strings.TrimSpace(envOrDefault("SMTP_PORT", "587")),
		Secure:   strings.EqualFold(strings.TrimSpace(os.Getenv("SMTP_SECURE")), "true"),
		AuthUser: strings.TrimSpace(os.Getenv("SMTP_AUTH_USER")),
		AuthPass: strings.TrimSpace(os.Getenv("SMTP_AUTH_PASS")),
		From:     strings.TrimSpace(os.Getenv("SMTP_FROM")),
		FromName: strings.TrimSpace(os.Getenv("SMTP_FROM_NAME")),
		HELO:     strings.TrimSpace(os.Getenv("SMTP_HELO")),
	}
	if cfg.Host == "" {
		return smtpConfig{}, errors.New("missing SMTP_HOST")
	}
	if cfg.Port == "" {
		cfg.Port = "587"
	}
	if cfg.From == "" {
		if cfg.AuthUser != "" {
			cfg.From = cfg.AuthUser
		} else {
			cfg.From = "no-reply@" + cfg.Host
		}
	}
	return cfg, nil
}

func sendSMTPPlainMail(ctx context.Context, cfg smtpConfig, recipient, subject, body string) error {
	addr := net.JoinHostPort(cfg.Host, cfg.Port)
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	var (
		conn net.Conn
		err  error
	)
	if cfg.Secure {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{ServerName: cfg.Host})
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("smtp dial: %w", err)
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		return fmt.Errorf("smtp client: %w", err)
	}
	defer client.Close()

	// Announce a real FQDN before any other command (net/smtp otherwise greets
	// as "localhost", which many MTAs reject or score as spam).
	if err := client.Hello(cfg.heloName()); err != nil {
		return fmt.Errorf("smtp helo: %w", err)
	}

	if !cfg.Secure {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(&tls.Config{ServerName: cfg.Host}); err != nil {
				return fmt.Errorf("smtp starttls: %w", err)
			}
		}
	}

	if cfg.AuthUser != "" {
		auth := smtp.PlainAuth("", cfg.AuthUser, cfg.AuthPass, cfg.Host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}

	if err := client.Mail(cfg.From); err != nil {
		return fmt.Errorf("smtp mail from: %w", err)
	}
	if err := client.Rcpt(recipient); err != nil {
		return fmt.Errorf("smtp rcpt to: %w", err)
	}

	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	messageID, err := generateMessageID(cfg.fromDomain())
	if err != nil {
		return fmt.Errorf("smtp message-id: %w", err)
	}
	message := buildMailMessage(cfg, recipient, subject, body, time.Now().Format(time.RFC1123Z), messageID)
	if _, err := writer.Write([]byte(message)); err != nil {
		_ = writer.Close()
		return fmt.Errorf("smtp write: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("smtp close writer: %w", err)
	}
	if err := client.Quit(); err != nil {
		return fmt.Errorf("smtp quit: %w", err)
	}
	return nil
}

func decodeBase64IfPrintable(value string) (string, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", false
	}
	decoded, err := base64.StdEncoding.DecodeString(trimmed)
	if err != nil || !utf8.Valid(decoded) {
		return "", false
	}
	decodedText := strings.TrimSpace(string(decoded))
	if decodedText == "" {
		return "", false
	}
	for _, r := range decodedText {
		if r == '\n' || r == '\r' || r == '\t' {
			continue
		}
		if r < 32 {
			return "", false
		}
	}
	return decodedText, true
}

func decodeBase64PDF(value string) ([]byte, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, false
	}

	if idx := strings.Index(value, ","); idx > 0 && strings.Contains(strings.ToLower(value[:idx]), "base64") {
		value = value[idx+1:]
	}
	value = strings.TrimSpace(value)

	var decoded []byte
	var err error
	decoded, err = base64.StdEncoding.DecodeString(value)
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(value)
	}
	if err != nil || len(decoded) == 0 {
		return nil, false
	}
	if !strings.HasPrefix(string(decoded), "%PDF-") {
		return nil, false
	}
	return decoded, true
}
