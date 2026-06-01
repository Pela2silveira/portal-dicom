package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	qrcode "github.com/skip2/go-qrcode"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type studyShareLinkSnapshot struct {
	ShareID          string
	PatientID        string
	StudyInstanceUID string
	ViewerKind       string
	Channel          string
	Status           string
	MaxUses          int
	ConsumedUses     int
	ExpiresAt        time.Time
	RevokedAt        sql.NullTime
	RecipientLabel   string
	RecipientContact string
}

func normalizeStudyShareChannel(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "share":
		return "share"
	case "qr":
		return "qr"
	case "whatsapp":
		return "whatsapp"
	case "email":
		return "email"
	case "copy":
		return "copy"
	default:
		return ""
	}
}

func buildStudyShareQRCodeDataURL(shareURL string) (string, error) {
	png, err := qrcode.Encode(shareURL, qrcode.Medium, 320)
	if err != nil {
		return "", err
	}

	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png), nil
}

func (a *App) createStudyShareLink(ctx context.Context, patientID, studyUID, viewerKind, channel string, reqBody PatientStudyShareRequest, r *http.Request, maxExpiresAt time.Time) (string, string, time.Time, int, error) {
	rawToken, err := randomToken(32)
	if err != nil {
		return "", "", time.Time{}, 0, err
	}

	duration := defaultStudyShareLinkDuration()
	if reqBody.ExpiresInHours > 0 {
		duration = time.Duration(reqBody.ExpiresInHours) * time.Hour
	}
	if duration > 7*24*time.Hour {
		duration = 7 * 24 * time.Hour
	}
	if duration < time.Hour {
		duration = time.Hour
	}
	expiresAt := time.Now().UTC().Add(duration)
	if !maxExpiresAt.IsZero() && maxExpiresAt.Before(expiresAt) {
		expiresAt = maxExpiresAt
	}
	if time.Now().UTC().After(expiresAt) {
		return "", "", time.Time{}, 0, errors.New("share link already expired")
	}

	maxUses := reqBody.MaxUses
	if maxUses <= 0 {
		maxUses = 10
	}
	if maxUses > 50 {
		maxUses = 50
	}

	_, err = a.db.ExecContext(ctx, `
		INSERT INTO study_share_links (
			token_hash, patient_id, study_instance_uid, viewer_kind, channel,
			status, max_uses, consumed_uses, expires_at, recipient_label, recipient_contact
		) VALUES (
			$1, CASE WHEN $2 = '' THEN NULL ELSE $2::uuid END, $3, $4, $5,
			'active', $6, 0, $7, NULLIF($8, ''), NULLIF($9, '')
		)
	`, tokenHash(rawToken), patientID, studyUID, viewerKind, channel, maxUses, expiresAt, strings.TrimSpace(reqBody.RecipientLabel), strings.TrimSpace(reqBody.RecipientContact))
	if err != nil {
		return "", "", time.Time{}, 0, err
	}

	shareURL := requestAbsoluteURL(r, "/share?t="+url.QueryEscape(rawToken))
	return shareURL, rawToken, expiresAt, maxUses, nil
}

func consumeAllowedSharedLink(link studyShareLinkSnapshot) bool {
	return (link.Status == "active" || link.Status == "consumed") && link.ConsumedUses > 0 && link.ConsumedUses <= link.MaxUses
}

func validateStudyShareLink(link studyShareLinkSnapshot, now time.Time) (viewerAccessGrantValidation, bool) {
	if link.RevokedAt.Valid {
		return viewerAccessGrantValidation{
			HTTPStatus: http.StatusForbidden,
			Message:    "share link revoked",
			Reason:     "revoked",
		}, true
	}
	if now.After(link.ExpiresAt) {
		return viewerAccessGrantValidation{
			HTTPStatus: http.StatusForbidden,
			Message:    "share link expired",
			Reason:     "expired",
		}, true
	}
	if link.ConsumedUses >= link.MaxUses {
		return viewerAccessGrantValidation{
			HTTPStatus: http.StatusForbidden,
			Message:    "share link already consumed",
			Reason:     "already_consumed",
		}, true
	}
	if link.Status != "active" {
		return viewerAccessGrantValidation{
			HTTPStatus: http.StatusForbidden,
			Message:    "share link inactive",
			Reason:     "inactive",
		}, true
	}
	return viewerAccessGrantValidation{}, false
}

func (a *App) consumeStudyShareLink(ctx context.Context, link studyShareLinkSnapshot) error {
	status := "active"
	if link.ConsumedUses+1 >= link.MaxUses {
		status = "consumed"
	}
	_, err := a.db.ExecContext(ctx, `
		UPDATE study_share_links
		SET consumed_uses = consumed_uses + 1,
		    first_opened_at = COALESCE(first_opened_at, now()),
		    last_opened_at = now(),
		    status = $2
		WHERE id = $1::uuid
	`, link.ShareID, status)
	return err
}

func (a *App) handleShareLanding(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	rawToken := strings.TrimSpace(r.URL.Query().Get("t"))
	if rawToken == "" {
		http.Error(w, "missing share token", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	link, err := a.studyShareLinkByToken(ctx, rawToken)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, "<!doctype html><html><body><h1>Enlace inválido</h1><p>El enlace compartido no existe o ya no está disponible.</p></body></html>")
			return
		}
		http.Error(w, "failed to load share link", http.StatusInternalServerError)
		return
	}
	if validation, denied := validateStudyShareLink(link, time.Now().UTC()); denied {
		statusCode := validation.HTTPStatus
		if statusCode == 0 {
			statusCode = http.StatusForbidden
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(statusCode)
		_, _ = io.WriteString(w, "<!doctype html><html><body><h1>Enlace no disponible</h1><p>"+validation.Message+"</p></body></html>")
		return
	}

	if err := a.consumeStudyShareLink(ctx, link); err != nil {
		http.Error(w, "failed to consume share link", http.StatusInternalServerError)
		return
	}

	setActionDim(r.Context(), "study_uid", link.StudyInstanceUID)
	setActionDim(r.Context(), "viewer_kind", link.ViewerKind)
	setActionDim(r.Context(), "channel", link.Channel)
	setShareLinkCookie(w, r, append(shareLinkTokensFromRequest(r), rawToken), link.ExpiresAt)
	http.Redirect(w, r, studyShareTokenRedirectURL(rawToken, link), http.StatusFound)
}

func setShareLinkCookie(w http.ResponseWriter, r *http.Request, tokens []string, expiresAt time.Time) {
	if w == nil {
		return
	}
	tokens = uniqueTrimmedStrings(tokens)
	if len(tokens) == 0 {
		clearShareLinkCookie(w, r)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     shareLinkCookieName,
		Value:    strings.Join(tokens, shareLinkCookieSeparator),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestIsSecure(r),
		Expires:  expiresAt.UTC(),
		MaxAge:   int(time.Until(expiresAt).Seconds()),
	})
}

func clearShareLinkCookie(w http.ResponseWriter, r *http.Request) {
	if w == nil {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     shareLinkCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestIsSecure(r),
		Expires:  time.Unix(0, 0).UTC(),
		MaxAge:   -1,
	})
}

func defaultStudyShareLinkDuration() time.Duration {
	return 72 * time.Hour
}
