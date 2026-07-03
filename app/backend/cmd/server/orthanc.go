package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type orthancModalityRequest struct {
	AET            string `json:"AET"`
	Host           string `json:"Host"`
	Port           int    `json:"Port"`
	RetrieveMethod string `json:"RetrieveMethod"`
}

var orthancQueryAnswerTagMappings = []struct {
	tag  string
	keys []string
}{
	{tag: "0020000D", keys: []string{"StudyInstanceUID", "0020,000D"}},
	{tag: "00100010", keys: []string{"PatientName", "0010,0010"}},
	{tag: "00100020", keys: []string{"PatientID", "0010,0020"}},
	{tag: "00100030", keys: []string{"PatientBirthDate", "0010,0030"}},
	{tag: "00100040", keys: []string{"PatientSex", "0010,0040"}},
	{tag: "00080020", keys: []string{"StudyDate", "0008,0020"}},
	{tag: "00081030", keys: []string{"StudyDescription", "0008,1030"}},
	{tag: "00080061", keys: []string{"ModalitiesInStudy", "0008,0061"}},
	{tag: "00080050", keys: []string{"AccessionNumber", "0008,0050"}},
	{tag: "00201208", keys: []string{"NumberOfStudyRelatedInstances", "0020,1208"}},
}

type ViewerAccessGrantResponse struct {
	Status           string `json:"status"`
	URL              string `json:"url"`
	StudyInstanceUID string `json:"study_instance_uid"`
	ViewerKind       string `json:"viewer_kind"`
	ExpiresAt        string `json:"expires_at"`
}

type viewerAccessGrantSnapshot struct {
	GrantID            string
	SubjectType        string
	PatientSessionID   string
	PhysicianSessionID string
	StudyInstanceUID   string
	ViewerKind         string
	Status             string
	MaxUses            int
	ConsumedUses       int
	ExpiresAt          time.Time
	RevokedAt          sql.NullTime
}

type viewerAccessGrantValidation struct {
	HTTPStatus int
	Message    string
	Reason     string
}

type orthancTokenValidationRequest struct {
	DICOMUID   string `json:"dicom-uid"`
	OrthancID  string `json:"orthanc-id"`
	Level      string `json:"level"`
	Method     string `json:"method"`
	URI        string `json:"uri"`
	TokenKey   string `json:"token-key"`
	TokenValue string `json:"token-value"`
	ServerID   string `json:"server-id"`
}

type orthancTokenValidationResponse struct {
	Granted  bool `json:"granted"`
	Validity int  `json:"validity"`
}

type orthancUserProfileRequest struct {
	TokenKey   string `json:"token-key"`
	TokenValue string `json:"token-value"`
	ServerID   string `json:"server-id"`
}

type orthancUserProfileResponse struct {
	Name             string   `json:"name"`
	AuthorizedLabels []string `json:"authorized-labels,omitempty"`
	Permissions      []string `json:"permissions"`
	Validity         int      `json:"validity"`
}

type orthancTokenResourceRequest struct {
	DICOMUID  string `json:"dicom-uid"`
	OrthancID string `json:"orthanc-id"`
	Level     string `json:"level"`
	URL       string `json:"url"`
}

type orthancTokenCreationRequest struct {
	ID               string                        `json:"id"`
	Type             string                        `json:"type"`
	Resources        []orthancTokenResourceRequest `json:"resources"`
	ExpirationDate   string                        `json:"expiration-date"`
	ValidityDuration int                           `json:"validity-duration"`
}

type orthancTokenCreationResponse struct {
	Request orthancTokenCreationRequest `json:"request"`
	Token   string                      `json:"token"`
	URL     string                      `json:"url,omitempty"`
}

type orthancTokenDecodeRequest struct {
	TokenKey   string `json:"token-key"`
	TokenValue string `json:"token-value"`
}

type orthancTokenDecodeResponse struct {
	TokenType   string `json:"token-type,omitempty"`
	RedirectURL string `json:"redirect-url,omitempty"`
	ErrorCode   string `json:"error-code,omitempty"`
}

func (a *App) handleOrthancTokenValidation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload orthancTokenValidationRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	response, reason, err := a.validateOrthancToken(ctx, payload)
	if err != nil {
		http.Error(w, "orthanc token validation failed", http.StatusInternalServerError)
		return
	}
	if !response.Granted {
		a.log("warn", "orthanc_token_validation_denied", map[string]any{
			"reason":     reason,
			"level":      payload.Level,
			"method":     payload.Method,
			"dicom_uid":  payload.DICOMUID,
			"orthanc_id": payload.OrthancID,
			"uri":        payload.URI,
		})
	}

	writeJSON(w, http.StatusOK, response)
}

func (a *App) handleOrthancUserProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload orthancUserProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	if !strings.EqualFold(strings.TrimSpace(payload.TokenKey), orthancInternalTokenHeader) {
		if strings.EqualFold(strings.TrimSpace(payload.TokenKey), "cookie") {
			ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
			defer cancel()

			for _, rawToken := range viewerGrantTokensFromCookieHeader(payload.TokenValue) {
				grant, err := a.viewerAccessGrantByToken(ctx, rawToken)
				if err != nil {
					if errors.Is(err, sql.ErrNoRows) {
						continue
					}
					http.Error(w, "orthanc user profile lookup failed", http.StatusInternalServerError)
					return
				}
				if _, denied := validateOrthancViewerGrant(grant, time.Now().UTC()); denied {
					continue
				}
				if _, denied, err := a.viewerGrantSessionValid(ctx, grant); err != nil {
					http.Error(w, "orthanc user profile lookup failed", http.StatusInternalServerError)
					return
				} else if denied {
					continue
				}

				writeJSON(w, http.StatusOK, orthancUserProfileResponse{
					Name:             "portal-viewer-grant",
					AuthorizedLabels: []string{"*"},
					Permissions:      []string{"viewer-tools-find"},
					Validity:         60,
				})
				return
			}
		}

		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if a.cfg.OrthancInternalToken == "" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(payload.TokenValue)), []byte(a.cfg.OrthancInternalToken)) != 1 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	writeJSON(w, http.StatusOK, orthancUserProfileResponse{
		Name:             "portal-backend-internal",
		AuthorizedLabels: []string{"*"},
		Permissions:      []string{"all"},
		Validity:         60,
	})
}

func (a *App) handleOrthancTokenCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tokenType := normalizeStudyShareTokenType(strings.TrimPrefix(strings.TrimSpace(r.URL.Path), "/api/orthanc-auth/tokens/"))
	if tokenType == "" {
		http.NotFound(w, r)
		return
	}

	var payload orthancTokenCreationRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	var (
		patientID string
		maxExpiry time.Time
	)
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if strings.TrimSpace(a.cfg.OrthancInternalToken) != "" &&
		subtle.ConstantTimeCompare([]byte(strings.TrimSpace(r.Header.Get(orthancInternalTokenHeader))), []byte(a.cfg.OrthancInternalToken)) == 1 {
		patientID = ""
		maxExpiry = time.Now().UTC().Add(7 * 24 * time.Hour)
	} else if session, patient, err := a.requirePatientSessionSummary(ctx, r); err == nil {
		patientID = patient.ID
		maxExpiry = session.ExpiresAt
	} else {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	if len(payload.Resources) != 1 {
		http.Error(w, "exactly one resource is required", http.StatusBadRequest)
		return
	}
	resource := payload.Resources[0]
	if strings.ToLower(strings.TrimSpace(resource.Level)) != "study" {
		http.Error(w, "only study resources are supported", http.StatusBadRequest)
		return
	}
	studyUID := strings.TrimSpace(resource.DICOMUID)
	if studyUID == "" {
		http.Error(w, "missing study uid", http.StatusBadRequest)
		return
	}

	if patientID != "" {
		authorized, err := a.patientStudyAvailableLocal(ctx, patientID, studyUID)
		if err != nil {
			http.Error(w, "failed to validate patient study access", http.StatusInternalServerError)
			return
		}
		if !authorized {
			http.Error(w, "study not available for share", http.StatusNotFound)
			return
		}
	}

	reqBody := PatientStudyShareRequest{
		ViewerKind:     viewerKindForStudyShareTokenType(tokenType),
		ExpiresInHours: payload.ValidityDuration / 3600,
		MaxUses:        10,
	}
	if strings.TrimSpace(payload.ExpirationDate) != "" {
		if parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(payload.ExpirationDate)); err == nil {
			reqBody.ExpiresInHours = int(time.Until(parsed).Hours())
		}
	}
	shareURL, rawToken, expiresAt, _, err := a.createStudyShareLink(ctx, patientID, studyUID, reqBody.ViewerKind, "share", reqBody, r, maxExpiry)
	if err != nil {
		http.Error(w, "failed to create resource token", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, orthancTokenCreationResponse{
		Request: orthancTokenCreationRequest{
			ID:               payload.ID,
			Type:             tokenType,
			Resources:        payload.Resources,
			ExpirationDate:   expiresAt.UTC().Format(time.RFC3339),
			ValidityDuration: int(time.Until(expiresAt).Seconds()),
		},
		Token: rawToken,
		URL:   shareURL,
	})
}

func (a *App) decodeStudyShareToken(ctx context.Context, rawToken string) (studyShareLinkSnapshot, orthancTokenDecodeResponse, error) {
	link, err := a.studyShareLinkByToken(ctx, rawToken)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return studyShareLinkSnapshot{}, orthancTokenDecodeResponse{ErrorCode: "unknown"}, nil
		}
		return studyShareLinkSnapshot{}, orthancTokenDecodeResponse{}, err
	}
	if validation, denied := validateStudyShareLink(link, time.Now().UTC()); denied {
		errorCode := "invalid"
		if validation.Reason == "expired" {
			errorCode = "expired"
		}
		return link, orthancTokenDecodeResponse{
			TokenType: studyShareTokenTypeForViewer(link.ViewerKind),
			ErrorCode: errorCode,
		}, nil
	}
	tokenType := studyShareTokenTypeForViewer(link.ViewerKind)
	return link, orthancTokenDecodeResponse{
		TokenType:   tokenType,
		RedirectURL: requestAbsoluteURL(nil, studyShareTokenRedirectURL(rawToken, link)),
	}, nil
}

func (a *App) handleOrthancTokenDecode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload orthancTokenDecodeRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	rawToken := strings.TrimSpace(payload.TokenValue)
	if rawToken == "" {
		writeJSON(w, http.StatusOK, orthancTokenDecodeResponse{ErrorCode: "invalid"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	_, response, err := a.decodeStudyShareToken(ctx, rawToken)
	if err != nil {
		http.Error(w, "failed to decode token", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func normalizeStudyShareTokenType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "stone-share":
		return "stone-share"
	case "ohif-share":
		return "ohif-share"
	default:
		return ""
	}
}

func studyShareTokenTypeForViewer(viewerKind string) string {
	if normalizeViewerKind(viewerKind) == "ohif" {
		return "ohif-share"
	}
	return "stone-share"
}

func viewerKindForStudyShareTokenType(tokenType string) string {
	if normalizeStudyShareTokenType(tokenType) == "ohif-share" {
		return "ohif"
	}
	return "stone"
}

func (a *App) studyShareLinkByToken(ctx context.Context, rawToken string) (studyShareLinkSnapshot, error) {
	var snapshot studyShareLinkSnapshot
	err := a.db.QueryRowContext(ctx, `
		SELECT id::text,
		       COALESCE(patient_id::text, ''),
		       study_instance_uid,
		       viewer_kind,
		       COALESCE(channel, ''),
		       status,
		       max_uses,
		       consumed_uses,
		       expires_at,
		       revoked_at,
		       COALESCE(recipient_label, ''),
		       COALESCE(recipient_contact, '')
		FROM study_share_links
		WHERE token_hash = $1
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, tokenHash(rawToken)).Scan(
		&snapshot.ShareID,
		&snapshot.PatientID,
		&snapshot.StudyInstanceUID,
		&snapshot.ViewerKind,
		&snapshot.Channel,
		&snapshot.Status,
		&snapshot.MaxUses,
		&snapshot.ConsumedUses,
		&snapshot.ExpiresAt,
		&snapshot.RevokedAt,
		&snapshot.RecipientLabel,
		&snapshot.RecipientContact,
	)
	return snapshot, err
}

func validateStudyShareLinkForOrthancUse(link studyShareLinkSnapshot, now time.Time) (viewerAccessGrantValidation, bool) {
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
	if link.ConsumedUses >= link.MaxUses && !consumeAllowedSharedLink(link) {
		return viewerAccessGrantValidation{
			HTTPStatus: http.StatusForbidden,
			Message:    "share link already consumed",
			Reason:     "already_consumed",
		}, true
	}
	if link.Status != "active" && !consumeAllowedSharedLink(link) {
		return viewerAccessGrantValidation{
			HTTPStatus: http.StatusForbidden,
			Message:    "share link inactive",
			Reason:     "inactive",
		}, true
	}
	return viewerAccessGrantValidation{}, false
}

func studyShareTokenRedirectURL(rawToken string, link studyShareLinkSnapshot) string {
	viewerURL := buildStoneViewerURL(link.StudyInstanceUID)
	if normalizeViewerKind(link.ViewerKind) == "ohif" {
		viewerURL = buildOHIFViewerURL(link.StudyInstanceUID)
	}
	separator := "?"
	if strings.Contains(viewerURL, "?") {
		separator = "&"
	}
	return viewerURL + separator + shareTokenKeyName + "=" + url.QueryEscape(strings.TrimSpace(rawToken))
}

func (a *App) handleViewerAccess(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	rawToken := strings.Trim(strings.TrimPrefix(r.URL.Path, "/viewer-access/"), "/")
	if rawToken == "" {
		http.NotFound(w, r)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	grant, err := a.viewerAccessGrantByToken(ctx, rawToken)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			a.log("warn", "viewer_access_grant_denied", map[string]any{
				"reason":    "not_found",
				"client_ip": clientIPForRateLimit(r),
				"path":      r.URL.Path,
			})
			http.Error(w, "viewer access grant not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to load viewer access grant", http.StatusInternalServerError)
		return
	}
	if validation, ok := validateViewerAccessGrant(grant, time.Now().UTC()); ok {
		a.log("warn", "viewer_access_grant_denied", map[string]any{
			"grant_id":           grant.GrantID,
			"reason":             validation.Reason,
			"subject_type":       grant.SubjectType,
			"viewer_kind":        grant.ViewerKind,
			"study_instance_uid": grant.StudyInstanceUID,
			"consumed_uses":      grant.ConsumedUses,
			"max_uses":           grant.MaxUses,
			"expires_at":         grant.ExpiresAt.UTC().Format(time.RFC3339),
			"client_ip":          clientIPForRateLimit(r),
		})
		http.Error(w, validation.Message, validation.HTTPStatus)
		return
	}

	switch grant.SubjectType {
	case "patient":
		if grant.PatientSessionID == "" {
			http.Error(w, "viewer access grant invalid", http.StatusForbidden)
			return
		}
		var expiresAt time.Time
		var status string
		err = a.db.QueryRowContext(ctx, `
			SELECT expires_at, status
			FROM patient_sessions
			WHERE id = $1::uuid
		`, grant.PatientSessionID).Scan(&expiresAt, &status)
		if err != nil || status != "active" || time.Now().UTC().After(expiresAt) {
			a.log("warn", "viewer_access_grant_denied", map[string]any{
				"grant_id":           grant.GrantID,
				"reason":             "patient_session_expired",
				"subject_type":       grant.SubjectType,
				"viewer_kind":        grant.ViewerKind,
				"study_instance_uid": grant.StudyInstanceUID,
				"client_ip":          clientIPForRateLimit(r),
			})
			http.Error(w, "patient session expired", http.StatusForbidden)
			return
		}
	case "physician":
		if grant.PhysicianSessionID == "" {
			http.Error(w, "viewer access grant invalid", http.StatusForbidden)
			return
		}
		var expiresAt time.Time
		var status string
		err = a.db.QueryRowContext(ctx, `
			SELECT expires_at, status
			FROM physician_sessions
			WHERE id = $1::uuid
		`, grant.PhysicianSessionID).Scan(&expiresAt, &status)
		if err != nil || status != "active" || time.Now().UTC().After(expiresAt) {
			a.log("warn", "viewer_access_grant_denied", map[string]any{
				"grant_id":           grant.GrantID,
				"reason":             "physician_session_expired",
				"subject_type":       grant.SubjectType,
				"viewer_kind":        grant.ViewerKind,
				"study_instance_uid": grant.StudyInstanceUID,
				"client_ip":          clientIPForRateLimit(r),
			})
			http.Error(w, "physician session expired", http.StatusForbidden)
			return
		}
	default:
		a.log("warn", "viewer_access_grant_denied", map[string]any{
			"grant_id":           grant.GrantID,
			"reason":             "unsupported_subject",
			"subject_type":       grant.SubjectType,
			"viewer_kind":        grant.ViewerKind,
			"study_instance_uid": grant.StudyInstanceUID,
			"client_ip":          clientIPForRateLimit(r),
		})
		http.Error(w, "unsupported viewer grant subject", http.StatusForbidden)
		return
	}

	isLocal, _, err := a.findOrthancStudy(ctx, grant.StudyInstanceUID)
	if err != nil {
		http.Error(w, "failed to validate local study availability", http.StatusInternalServerError)
		return
	}
	if !isLocal {
		a.log("warn", "viewer_access_grant_denied", map[string]any{
			"grant_id":           grant.GrantID,
			"reason":             "study_not_local",
			"subject_type":       grant.SubjectType,
			"viewer_kind":        grant.ViewerKind,
			"study_instance_uid": grant.StudyInstanceUID,
			"client_ip":          clientIPForRateLimit(r),
		})
		http.Error(w, "study no longer available locally", http.StatusNotFound)
		return
	}

	if err := a.consumeViewerAccessGrant(ctx, grant); err != nil {
		http.Error(w, "failed to consume viewer access grant", http.StatusInternalServerError)
		return
	}

	setViewerGrantCookie(w, r, append(viewerGrantTokensFromRequest(r), rawToken), grant.ExpiresAt)

	redirectURL := buildStoneViewerURL(grant.StudyInstanceUID)
	if grant.ViewerKind == "ohif" {
		redirectURL = buildOHIFViewerURL(grant.StudyInstanceUID)
	}
	a.log("info", "viewer_access_grant_consumed", map[string]any{
		"grant_id":           grant.GrantID,
		"subject_type":       grant.SubjectType,
		"viewer_kind":        grant.ViewerKind,
		"study_instance_uid": grant.StudyInstanceUID,
		"redirect_url":       redirectURL,
		"client_ip":          clientIPForRateLimit(r),
	})
	http.Redirect(w, r, redirectURL, http.StatusFound)
}

func (a *App) checkOrthanc(ctx context.Context) bool {
	if strings.TrimSpace(a.cfg.OrthancURL) == "" {
		a.log("error", "orthanc_unconfigured", map[string]any{})
		return false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.cfg.OrthancURL+"/system", nil)
	if err != nil {
		a.log("error", "orthanc_request_build_failed", map[string]any{"error": err.Error()})
		return false
	}
	a.applyOrthancInternalRequestAuth(req)

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

func (a *App) checkRemotePACSViaOrthancEcho(parent context.Context, node PACSNodeConfig, resolved PACSNodeResolvedConfig) bool {
	ctx, cancel := context.WithTimeout(parent, dimseEchoHealthTimeout)
	defer cancel()

	if err := a.ensureOrthancModality(ctx, node); err != nil {
		a.log("error", "remote_pacs_echo_modality_failed", map[string]any{
			"node_id": resolved.ID,
			"mode":    resolved.HealthMode,
			"error":   err.Error(),
		})
		return false
	}

	payload, err := json.Marshal(map[string]any{
		"Timeout": int(dimseEchoHealthTimeout / time.Second),
	})
	if err != nil {
		a.log("error", "remote_pacs_echo_payload_failed", map[string]any{
			"node_id": resolved.ID,
			"mode":    resolved.HealthMode,
			"error":   err.Error(),
		})
		return false
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(a.cfg.OrthancURL, "/")+"/modalities/"+url.PathEscape(resolved.ID)+"/echo", strings.NewReader(string(payload)))
	if err != nil {
		a.log("error", "remote_pacs_echo_request_build_failed", map[string]any{
			"node_id": resolved.ID,
			"mode":    resolved.HealthMode,
			"error":   err.Error(),
		})
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	a.applyOrthancInternalRequestAuth(req)

	return a.executeOrthancEcho(ctx, node, resolved, req, true)
}

func (a *App) orthancModalityPayload(node PACSNodeConfig) ([]byte, string, error) {
	resolved := node.Resolved()
	payload, err := json.Marshal(orthancModalityRequest{
		AET:            resolved.AET,
		Host:           resolved.DICOMHost,
		Port:           resolved.DICOMPort,
		RetrieveMethod: "C-GET",
	})
	if err != nil {
		return nil, "", err
	}
	return payload, string(payload), nil
}

func (a *App) isOrthancModalityCached(nodeID, signature string) bool {
	if a == nil {
		return false
	}
	a.orthancModalityMu.Lock()
	defer a.orthancModalityMu.Unlock()
	return a.orthancModalities[nodeID] == signature
}

func (a *App) cacheOrthancModality(nodeID, signature string) {
	if a == nil {
		return
	}
	a.orthancModalityMu.Lock()
	defer a.orthancModalityMu.Unlock()
	a.orthancModalities[nodeID] = signature
}

func (a *App) invalidateOrthancModality(nodeID string) {
	if a == nil {
		return
	}
	a.orthancModalityMu.Lock()
	defer a.orthancModalityMu.Unlock()
	delete(a.orthancModalities, nodeID)
}

func orthancModalityMissing(statusCode int, body string) bool {
	if statusCode != http.StatusNotFound {
		return false
	}
	normalized := strings.ToLower(strings.TrimSpace(body))
	return normalized == "" || strings.Contains(normalized, "unknown modality") || strings.Contains(normalized, "not found")
}

func sessionCookieToken(r *http.Request, cookieName string) string {
	if r == nil || strings.TrimSpace(cookieName) == "" {
		return ""
	}
	cookie, err := r.Cookie(cookieName)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(cookie.Value)
}

func viewerGrantTokensFromCookieValue(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return uniqueTrimmedStrings(strings.Split(value, viewerGrantCookieSeparator))
}

func viewerGrantTokensFromRequest(r *http.Request) []string {
	if r == nil {
		return nil
	}
	cookie, err := r.Cookie(viewerGrantCookieName)
	if err != nil {
		return nil
	}
	return viewerGrantTokensFromCookieValue(cookie.Value)
}

func viewerGrantTokensFromCookieHeader(rawCookieHeader string) []string {
	rawCookieHeader = strings.TrimSpace(rawCookieHeader)
	if rawCookieHeader == "" {
		return nil
	}
	req := &http.Request{Header: http.Header{"Cookie": []string{rawCookieHeader}}}
	for _, cookie := range req.Cookies() {
		if cookie != nil && cookie.Name == viewerGrantCookieName {
			return viewerGrantTokensFromCookieValue(cookie.Value)
		}
	}
	return nil
}

func shareLinkTokensFromRequest(r *http.Request) []string {
	if r == nil {
		return nil
	}
	cookie, err := r.Cookie(shareLinkCookieName)
	if err != nil {
		return nil
	}
	return viewerGrantTokensFromCookieValue(strings.ReplaceAll(cookie.Value, shareLinkCookieSeparator, viewerGrantCookieSeparator))
}

func shareLinkTokensFromCookieHeader(rawCookieHeader string) []string {
	rawCookieHeader = strings.TrimSpace(rawCookieHeader)
	if rawCookieHeader == "" {
		return nil
	}
	req := &http.Request{Header: http.Header{"Cookie": []string{rawCookieHeader}}}
	for _, cookie := range req.Cookies() {
		if cookie != nil && cookie.Name == shareLinkCookieName {
			return viewerGrantTokensFromCookieValue(strings.ReplaceAll(cookie.Value, shareLinkCookieSeparator, viewerGrantCookieSeparator))
		}
	}
	return nil
}

func setViewerGrantCookie(w http.ResponseWriter, r *http.Request, tokens []string, expiresAt time.Time) {
	if w == nil {
		return
	}
	tokens = uniqueTrimmedStrings(tokens)
	if len(tokens) == 0 {
		clearViewerGrantCookie(w, r)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     viewerGrantCookieName,
		Value:    strings.Join(tokens, viewerGrantCookieSeparator),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestIsSecure(r),
		Expires:  expiresAt.UTC(),
		MaxAge:   int(time.Until(expiresAt).Seconds()),
	})
}

func clearViewerGrantCookie(w http.ResponseWriter, r *http.Request) {
	if w == nil {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     viewerGrantCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestIsSecure(r),
		Expires:  time.Unix(0, 0).UTC(),
		MaxAge:   -1,
	})
}

func randomToken(numBytes int) (string, error) {
	if numBytes <= 0 {
		numBytes = 32
	}
	buf := make([]byte, numBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func tokenHash(raw string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(raw)))
	return hex.EncodeToString(sum[:])
}

func viewerAccessGrantDuration() time.Duration {
	return 5 * time.Minute
}

func normalizeViewerKind(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "stone", "":
		return "stone"
	case "ohif":
		return "ohif"
	default:
		return ""
	}
}

func tokenizeFuzzySearch(value string) []string {
	normalized := normalizeFuzzySearchText(value)
	if normalized == "" {
		return nil
	}
	return strings.Fields(normalized)
}

func (a *App) createViewerAccessGrant(ctx context.Context, subjectType, patientSessionID, physicianSessionID, studyUID, viewerKind string, r *http.Request, maxExpiresAt time.Time) (string, time.Time, error) {
	rawToken, err := randomToken(32)
	if err != nil {
		return "", time.Time{}, err
	}
	expiresAt := time.Now().UTC().Add(viewerAccessGrantDuration())
	if !maxExpiresAt.IsZero() && maxExpiresAt.Before(expiresAt) {
		expiresAt = maxExpiresAt
	}
	if time.Now().UTC().After(expiresAt) {
		return "", time.Time{}, errors.New("viewer access grant already expired")
	}
	_, err = a.db.ExecContext(ctx, `
		INSERT INTO viewer_access_grants (
			token_hash, subject_type, patient_session_id, physician_session_id,
			study_instance_uid, viewer_kind, status, max_uses, consumed_uses,
			expires_at, client_ip, user_agent
		) VALUES (
			$1, $2, CASE WHEN $3 = '' THEN NULL ELSE $3::uuid END, CASE WHEN $4 = '' THEN NULL ELSE $4::uuid END,
			$5, $6, 'active', 1, 0,
			$7, NULLIF($8, '')::inet, NULLIF($9, '')
		)
	`, tokenHash(rawToken), subjectType, patientSessionID, physicianSessionID, studyUID, viewerKind, expiresAt, clientIPForRateLimit(r), strings.TrimSpace(r.UserAgent()))
	if err != nil {
		return "", time.Time{}, err
	}
	accessURL := "/viewer-access/" + url.PathEscape(rawToken)
	a.log("info", "viewer_access_grant_created", map[string]any{
		"subject_type":         subjectType,
		"patient_session_id":   patientSessionID,
		"physician_session_id": physicianSessionID,
		"study_instance_uid":   studyUID,
		"viewer_kind":          viewerKind,
		"expires_at":           expiresAt.UTC().Format(time.RFC3339),
		"access_url":           accessURL,
		"client_ip":            clientIPForRateLimit(r),
	})
	return accessURL, expiresAt, nil
}

func (a *App) viewerAccessGrantByToken(ctx context.Context, rawToken string) (viewerAccessGrantSnapshot, error) {
	var snapshot viewerAccessGrantSnapshot
	err := a.db.QueryRowContext(ctx, `
		SELECT id::text, subject_type,
		       COALESCE(patient_session_id::text, ''),
		       COALESCE(physician_session_id::text, ''),
		       study_instance_uid, viewer_kind, status, max_uses, consumed_uses, expires_at, revoked_at
		FROM viewer_access_grants
		WHERE token_hash = $1
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, tokenHash(rawToken)).Scan(
		&snapshot.GrantID,
		&snapshot.SubjectType,
		&snapshot.PatientSessionID,
		&snapshot.PhysicianSessionID,
		&snapshot.StudyInstanceUID,
		&snapshot.ViewerKind,
		&snapshot.Status,
		&snapshot.MaxUses,
		&snapshot.ConsumedUses,
		&snapshot.ExpiresAt,
		&snapshot.RevokedAt,
	)
	return snapshot, err
}

func (a *App) consumeViewerAccessGrant(ctx context.Context, grant viewerAccessGrantSnapshot) error {
	status := "active"
	if grant.ConsumedUses+1 >= grant.MaxUses {
		status = "consumed"
	}
	_, err := a.db.ExecContext(ctx, `
		UPDATE viewer_access_grants
		SET consumed_uses = consumed_uses + 1,
		    first_opened_at = COALESCE(first_opened_at, now()),
		    last_opened_at = now(),
		    status = $2
		WHERE id = $1::uuid
	`, grant.GrantID, status)
	return err
}

func validateViewerAccessGrant(grant viewerAccessGrantSnapshot, now time.Time) (viewerAccessGrantValidation, bool) {
	if grant.RevokedAt.Valid {
		return viewerAccessGrantValidation{
			HTTPStatus: http.StatusForbidden,
			Message:    "viewer access grant revoked",
			Reason:     "revoked",
		}, true
	}
	if now.After(grant.ExpiresAt) {
		return viewerAccessGrantValidation{
			HTTPStatus: http.StatusForbidden,
			Message:    "viewer access grant expired",
			Reason:     "expired",
		}, true
	}
	if grant.ConsumedUses >= grant.MaxUses {
		return viewerAccessGrantValidation{
			HTTPStatus: http.StatusForbidden,
			Message:    "viewer access grant already consumed",
			Reason:     "already_consumed",
		}, true
	}
	if grant.Status != "active" {
		return viewerAccessGrantValidation{
			HTTPStatus: http.StatusForbidden,
			Message:    "viewer access grant inactive",
			Reason:     "inactive",
		}, true
	}
	return viewerAccessGrantValidation{}, false
}

func (a *App) viewerGrantSessionValid(ctx context.Context, grant viewerAccessGrantSnapshot) (viewerAccessGrantValidation, bool, error) {
	switch grant.SubjectType {
	case "patient":
		if grant.PatientSessionID == "" {
			return viewerAccessGrantValidation{
				HTTPStatus: http.StatusForbidden,
				Message:    "viewer access grant invalid",
				Reason:     "missing_patient_session",
			}, true, nil
		}
		var expiresAt time.Time
		var status string
		err := a.db.QueryRowContext(ctx, `
			SELECT expires_at, status
			FROM patient_sessions
			WHERE id = $1::uuid
		`, grant.PatientSessionID).Scan(&expiresAt, &status)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return viewerAccessGrantValidation{
					HTTPStatus: http.StatusForbidden,
					Message:    "patient session expired",
					Reason:     "patient_session_missing",
				}, true, nil
			}
			return viewerAccessGrantValidation{}, false, err
		}
		if status != "active" || time.Now().UTC().After(expiresAt) {
			return viewerAccessGrantValidation{
				HTTPStatus: http.StatusForbidden,
				Message:    "patient session expired",
				Reason:     "patient_session_expired",
			}, true, nil
		}
	case "physician":
		if grant.PhysicianSessionID == "" {
			return viewerAccessGrantValidation{
				HTTPStatus: http.StatusForbidden,
				Message:    "viewer access grant invalid",
				Reason:     "missing_physician_session",
			}, true, nil
		}
		var expiresAt time.Time
		var status string
		err := a.db.QueryRowContext(ctx, `
			SELECT expires_at, status
			FROM physician_sessions
			WHERE id = $1::uuid
		`, grant.PhysicianSessionID).Scan(&expiresAt, &status)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return viewerAccessGrantValidation{
					HTTPStatus: http.StatusForbidden,
					Message:    "physician session expired",
					Reason:     "physician_session_missing",
				}, true, nil
			}
			return viewerAccessGrantValidation{}, false, err
		}
		if status != "active" || time.Now().UTC().After(expiresAt) {
			return viewerAccessGrantValidation{
				HTTPStatus: http.StatusForbidden,
				Message:    "physician session expired",
				Reason:     "physician_session_expired",
			}, true, nil
		}
	default:
		return viewerAccessGrantValidation{
			HTTPStatus: http.StatusForbidden,
			Message:    "unsupported viewer grant subject",
			Reason:     "unsupported_subject",
		}, true, nil
	}

	return viewerAccessGrantValidation{}, false, nil
}

func validateOrthancViewerGrant(grant viewerAccessGrantSnapshot, now time.Time) (viewerAccessGrantValidation, bool) {
	if grant.RevokedAt.Valid {
		return viewerAccessGrantValidation{
			HTTPStatus: http.StatusForbidden,
			Message:    "viewer access grant revoked",
			Reason:     "revoked",
		}, true
	}
	if now.After(grant.ExpiresAt) {
		return viewerAccessGrantValidation{
			HTTPStatus: http.StatusForbidden,
			Message:    "viewer access grant expired",
			Reason:     "expired",
		}, true
	}
	if grant.Status != "active" && grant.Status != "consumed" {
		return viewerAccessGrantValidation{
			HTTPStatus: http.StatusForbidden,
			Message:    "viewer access grant inactive",
			Reason:     "inactive",
		}, true
	}
	if grant.Status == "consumed" && grant.ConsumedUses <= 0 {
		return viewerAccessGrantValidation{
			HTTPStatus: http.StatusForbidden,
			Message:    "viewer access grant inactive",
			Reason:     "invalid_consumed_state",
		}, true
	}
	return viewerAccessGrantValidation{}, false
}

func orthancSystemURIAllowed(uri string) bool {
	uri = strings.TrimSpace(uri)
	if uri == "" {
		return true
	}
	return strings.HasPrefix(uri, "/stone-webviewer/")
}

func (a *App) applyOrthancInternalRequestAuth(req *http.Request) {
	if req == nil {
		return
	}
	if a.cfg.OrthancUser != "" {
		req.SetBasicAuth(a.cfg.OrthancUser, a.cfg.OrthancPass)
	}
	if a.cfg.OrthancInternalToken != "" {
		req.Header.Set(orthancInternalTokenHeader, a.cfg.OrthancInternalToken)
	}
}

func validateStudyShareLinkForOrthanc(link studyShareLinkSnapshot, payload orthancTokenValidationRequest) (orthancTokenValidationResponse, string) {
	method := strings.ToLower(strings.TrimSpace(payload.Method))
	level := strings.ToLower(strings.TrimSpace(payload.Level))
	dicomUID := strings.TrimSpace(payload.DICOMUID)
	uri := strings.TrimSpace(payload.URI)

	if method != "" && method != "get" && method != "head" && method != "post" {
		return orthancTokenValidationResponse{Granted: false, Validity: 1}, "unsupported_method"
	}

	switch level {
	case "", "system":
		if orthancSystemURIAllowed(uri) {
			return orthancTokenValidationResponse{Granted: true, Validity: 60}, "granted_system_share"
		}
	case "study":
		if dicomUID != "" && subtle.ConstantTimeCompare([]byte(dicomUID), []byte(link.StudyInstanceUID)) == 1 {
			return orthancTokenValidationResponse{Granted: true, Validity: 60}, "granted_study_share"
		}
	}

	switch level {
	case "", "system":
		return orthancTokenValidationResponse{Granted: false, Validity: 1}, "system_uri_not_allowed"
	case "study":
		return orthancTokenValidationResponse{Granted: false, Validity: 1}, "study_not_granted"
	default:
		return orthancTokenValidationResponse{Granted: false, Validity: 1}, "unsupported_level"
	}
}

func (a *App) validateOrthancToken(ctx context.Context, payload orthancTokenValidationRequest) (orthancTokenValidationResponse, string, error) {
	if strings.EqualFold(strings.TrimSpace(payload.TokenKey), orthancInternalTokenHeader) {
		if a.cfg.OrthancInternalToken == "" {
			return orthancTokenValidationResponse{Granted: false, Validity: 1}, "internal_token_not_configured", nil
		}
		if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(payload.TokenValue)), []byte(a.cfg.OrthancInternalToken)) == 1 {
			return orthancTokenValidationResponse{Granted: true, Validity: 60}, "granted_internal_token", nil
		}
		return orthancTokenValidationResponse{Granted: false, Validity: 1}, "invalid_internal_token", nil
	}

	if strings.EqualFold(strings.TrimSpace(payload.TokenKey), shareTokenKeyName) {
		link, err := a.studyShareLinkByToken(ctx, payload.TokenValue)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return orthancTokenValidationResponse{Granted: false, Validity: 1}, "unknown_share_token", nil
			}
			return orthancTokenValidationResponse{}, "", err
		}
		if validation, denied := validateStudyShareLinkForOrthancUse(link, time.Now().UTC()); denied {
			return orthancTokenValidationResponse{Granted: false, Validity: 1}, validation.Reason, nil
		}
		response, reason := validateStudyShareLinkForOrthanc(link, payload)
		return response, reason, nil
	}

	method := strings.ToLower(strings.TrimSpace(payload.Method))

	shareTokens := shareLinkTokensFromCookieHeader(payload.TokenValue)
	for _, rawToken := range shareTokens {
		link, err := a.studyShareLinkByToken(ctx, rawToken)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			return orthancTokenValidationResponse{}, "", err
		}
		if validation, denied := validateStudyShareLinkForOrthancUse(link, time.Now().UTC()); denied {
			_ = validation
			continue
		}
		response, reason := validateStudyShareLinkForOrthanc(link, payload)
		if response.Granted {
			return response, reason, nil
		}
	}

	if method != "" && method != "get" && method != "head" {
		return orthancTokenValidationResponse{Granted: false, Validity: 1}, "unsupported_method", nil
	}

	tokens := viewerGrantTokensFromCookieHeader(payload.TokenValue)
	if len(tokens) == 0 {
		return orthancTokenValidationResponse{Granted: false, Validity: 1}, "missing_viewer_grant_cookie", nil
	}

	level := strings.ToLower(strings.TrimSpace(payload.Level))
	dicomUID := strings.TrimSpace(payload.DICOMUID)
	uri := strings.TrimSpace(payload.URI)

	for _, rawToken := range tokens {
		grant, err := a.viewerAccessGrantByToken(ctx, rawToken)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			return orthancTokenValidationResponse{}, "", err
		}

		if validation, denied := validateOrthancViewerGrant(grant, time.Now().UTC()); denied {
			_ = validation
			continue
		}
		if validation, denied, err := a.viewerGrantSessionValid(ctx, grant); err != nil {
			return orthancTokenValidationResponse{}, "", err
		} else if denied {
			_ = validation
			continue
		}

		switch level {
		case "", "system":
			if orthancSystemURIAllowed(uri) {
				return orthancTokenValidationResponse{Granted: true, Validity: 1}, "granted_system", nil
			}
		case "study":
			if dicomUID != "" && subtle.ConstantTimeCompare([]byte(dicomUID), []byte(grant.StudyInstanceUID)) == 1 {
				return orthancTokenValidationResponse{Granted: true, Validity: 1}, "granted_study", nil
			}
		}
	}

	switch level {
	case "", "system":
		return orthancTokenValidationResponse{Granted: false, Validity: 1}, "system_uri_not_allowed", nil
	case "study":
		return orthancTokenValidationResponse{Granted: false, Validity: 1}, "study_not_granted", nil
	default:
		return orthancTokenValidationResponse{Granted: false, Validity: 1}, "unsupported_level", nil
	}
}

func (a *App) fetchPACSBearerToken(ctx context.Context, node PACSNodeConfig) (string, error) {
	resolved := node.Resolved()
	if resolved.Auth.Type == "" {
		return "", nil
	}
	if resolved.Auth.Type != "keycloak_client_credentials" {
		return "", fmt.Errorf("unsupported pacs auth type %q", resolved.Auth.Type)
	}

	clientID := strings.TrimSpace(os.Getenv(resolved.Auth.ClientIDEnv))
	clientSecret := strings.TrimSpace(os.Getenv(resolved.Auth.ClientSecretEnv))
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)

	tokenStartedAt := time.Now()
	tokenURL, err := url.Parse(resolved.Auth.TokenURL)
	if err != nil {
		return "", fmt.Errorf("parse token url: %w", err)
	}
	a.log("info", "pacs_token_request_started", map[string]any{
		"node_id":       node.ID,
		"auth_type":     resolved.Auth.Type,
		"token_host":    tokenURL.Host,
		"token_path":    tokenURL.Path,
		"client_id_env": resolved.Auth.ClientIDEnv,
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, resolved.Auth.TokenURL, strings.NewReader(form.Encode()))
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

	a.log("info", "pacs_token_request_completed", map[string]any{
		"node_id":     node.ID,
		"auth_type":   resolved.Auth.Type,
		"token_host":  tokenURL.Host,
		"duration_ms": time.Since(tokenStartedAt).Milliseconds(),
	})

	return payload.AccessToken, nil
}

func (a *App) findOrthancStudy(ctx context.Context, studyUID string) (bool, string, error) {
	lookupReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(a.cfg.OrthancURL, "/")+"/tools/find", strings.NewReader(`{"Level":"Study","Query":{"StudyInstanceUID":"`+studyUID+`"}}`))
	if err != nil {
		return false, "", fmt.Errorf("build orthanc study lookup request: %w", err)
	}
	lookupReq.Header.Set("Content-Type", "application/json")
	a.applyOrthancInternalRequestAuth(lookupReq)
	lookupRes, err := a.httpClient.Do(lookupReq)
	if err != nil {
		return false, "", fmt.Errorf("execute orthanc study lookup request: %w", err)
	}
	defer lookupRes.Body.Close()
	if lookupRes.StatusCode < 200 || lookupRes.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(lookupRes.Body, 2048))
		return false, "", fmt.Errorf("orthanc study lookup bad status %d: %s", lookupRes.StatusCode, strings.TrimSpace(string(body)))
	}
	var ids []string
	if err := json.NewDecoder(lookupRes.Body).Decode(&ids); err != nil {
		return false, "", fmt.Errorf("decode orthanc study lookup response: %w", err)
	}
	if len(ids) == 0 {
		return false, "", nil
	}

	return true, ids[0], nil
}

func sanitizeDownloadToken(value string) string {
	replacer := strings.NewReplacer("/", "_", "\\", "_", " ", "_", "\"", "", "'", "")
	return replacer.Replace(strings.TrimSpace(value))
}

type orthancStudyResource struct {
	Series []string `json:"Series"`
}

type orthancSeriesResource struct {
	Instances []string `json:"Instances"`
}

func (a *App) getOrthancJSON(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(a.cfg.OrthancURL, "/")+path, nil)
	if err != nil {
		return err
	}
	a.applyOrthancInternalRequestAuth(req)

	res, err := a.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return fmt.Errorf("orthanc get %s bad status %d: %s", path, res.StatusCode, strings.TrimSpace(string(body)))
	}

	return json.NewDecoder(res.Body).Decode(out)
}

func (a *App) getOrthancPreviewDataURL(ctx context.Context, instanceID string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(a.cfg.OrthancURL, "/")+"/instances/"+url.PathEscape(instanceID)+"/preview", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "image/jpeg")
	a.applyOrthancInternalRequestAuth(req)

	res, err := a.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return "", fmt.Errorf("orthanc preview bad status %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}

	contentType := strings.TrimSpace(res.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = "image/jpeg"
	}
	imageBytes, err := io.ReadAll(io.LimitReader(res.Body, 8*1024*1024))
	if err != nil {
		return "", err
	}

	return "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(imageBytes), nil
}

type orthancJobResponse struct {
	ID               string          `json:"ID"`
	State            string          `json:"State"`
	Progress         int             `json:"Progress"`
	ErrorCode        int             `json:"ErrorCode"`
	ErrorDescription string          `json:"ErrorDescription"`
	Content          json.RawMessage `json:"Content"`
}

type orthancRetrieveStatus struct {
	State    string
	Phase    string
	Progress int
	Error    string
}

func mapOrthancJobStateToRetrievePhase(state string) string {
	switch strings.TrimSpace(state) {
	case "Pending", "Retry":
		return "preparing"
	case "Running":
		return "retrieving"
	case "Paused":
		return "paused"
	case "Success":
		return "verifying"
	case "Failure":
		return "failed"
	default:
		return "retrieving"
	}
}

func (a *App) fetchOrthancJobStatus(ctx context.Context, orthancJobID string) (orthancRetrieveStatus, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(a.cfg.OrthancURL, "/")+"/jobs/"+url.PathEscape(orthancJobID), nil)
	if err != nil {
		return orthancRetrieveStatus{}, err
	}
	a.applyOrthancInternalRequestAuth(req)

	res, err := a.httpClient.Do(req)
	if err != nil {
		return orthancRetrieveStatus{}, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return orthancRetrieveStatus{}, fmt.Errorf("orthanc job bad status %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload orthancJobResponse
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return orthancRetrieveStatus{}, err
	}

	progress := payload.Progress
	if progress < 0 {
		progress = 0
	}
	if progress > 100 {
		progress = 100
	}

	return orthancRetrieveStatus{
		State:    strings.TrimSpace(payload.State),
		Phase:    mapOrthancJobStateToRetrievePhase(payload.State),
		Progress: progress,
		Error:    strings.TrimSpace(payload.ErrorDescription),
	}, nil
}

func (a *App) monitorOrthancRetrieveJob(ctx context.Context, jobID, orthancJobID, studyUID string) (string, error) {
	ticker := time.NewTicker(a.retrieveProgressPollInterval())
	defer ticker.Stop()

	lastState := ""
	lastProgress := -1

	checkOnce := func() (string, bool, error) {
		status, err := a.fetchOrthancJobStatus(ctx, orthancJobID)
		if err != nil {
			return "", false, err
		}

		if status.State != lastState || status.Progress != lastProgress {
			if err := a.updateRetrieveJobStatus(ctx, jobID, "running", status.Phase, status.Progress, "", orthancJobID, "", 0, false); err != nil {
				a.log("error", "retrieve_job_progress_update_failed", map[string]any{
					"job_id":         jobID,
					"orthanc_job_id": orthancJobID,
					"error":          err.Error(),
				})
			}
			lastState = status.State
			lastProgress = status.Progress
		}

		switch status.State {
		case "Success":
			localReady, orthancStudyID, err := a.waitForStudyInOrthanc(ctx, studyUID, 2*time.Second, 20*time.Second)
			if err != nil {
				return "", false, err
			}
			if !localReady {
				return "", false, errors.New("study not available in orthanc after c-get")
			}
			return orthancStudyID, true, nil
		case "Failure":
			if status.Error == "" {
				status.Error = "orthanc retrieve job failed"
			}
			return "", false, errors.New(status.Error)
		}

		return "", false, nil
	}

	for {
		orthancStudyID, done, err := checkOnce()
		if err != nil {
			return "", err
		}
		if done {
			return orthancStudyID, nil
		}

		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-ticker.C:
		}
	}
}

func (a *App) ensureOrthancModality(ctx context.Context, node PACSNodeConfig) error {
	payload, signature, err := a.orthancModalityPayload(node)
	if err != nil {
		return err
	}

	if a.isOrthancModalityCached(node.ID, signature) {
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, strings.TrimRight(a.cfg.OrthancURL, "/")+"/modalities/"+url.PathEscape(node.ID), strings.NewReader(string(payload)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	a.applyOrthancInternalRequestAuth(req)

	res, err := a.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return fmt.Errorf("orthanc modality put bad status %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}
	a.cacheOrthancModality(node.ID, signature)
	return nil
}

func (a *App) executeOrthancEcho(ctx context.Context, node PACSNodeConfig, resolved PACSNodeResolvedConfig, req *http.Request, allowRefresh bool) bool {
	res, err := a.httpClient.Do(req)
	if err != nil {
		a.log("error", "remote_pacs_echo_unreachable", map[string]any{
			"node_id": resolved.ID,
			"mode":    resolved.HealthMode,
			"error":   err.Error(),
		})
		return false
	}
	defer res.Body.Close()

	ok := res.StatusCode >= 200 && res.StatusCode < 300
	if ok {
		return true
	}

	body, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
	bodyText := strings.TrimSpace(string(body))
	if allowRefresh && orthancModalityMissing(res.StatusCode, bodyText) {
		a.invalidateOrthancModality(node.ID)
		if err := a.ensureOrthancModality(ctx, node); err != nil {
			a.log("error", "remote_pacs_echo_modality_refresh_failed", map[string]any{
				"node_id": resolved.ID,
				"mode":    resolved.HealthMode,
				"error":   err.Error(),
			})
			return false
		}

		retryReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(a.cfg.OrthancURL, "/")+"/modalities/"+url.PathEscape(resolved.ID)+"/echo", strings.NewReader(fmt.Sprintf(`{"Timeout":%d}`, int(dimseEchoHealthTimeout/time.Second))))
		if err != nil {
			a.log("error", "remote_pacs_echo_request_build_failed", map[string]any{
				"node_id": resolved.ID,
				"mode":    resolved.HealthMode,
				"error":   err.Error(),
			})
			return false
		}
		retryReq.Header.Set("Content-Type", "application/json")
		a.applyOrthancInternalRequestAuth(retryReq)
		return a.executeOrthancEcho(ctx, node, resolved, retryReq, false)
	}

	a.log("error", "remote_pacs_echo_bad_status", map[string]any{
		"node_id":     resolved.ID,
		"mode":        resolved.HealthMode,
		"status_code": res.StatusCode,
		"body":        bodyText,
	})
	return false
}

func (a *App) startOrthancCGet(ctx context.Context, node PACSNodeConfig, studyInstanceUID string) (string, error) {
	return a.startOrthancCGetWithRefresh(ctx, node, studyInstanceUID, true)
}

func (a *App) runOrthancStudyCFind(ctx context.Context, node PACSNodeConfig, filters PhysicianSearchFilters) ([]qidoResponseItem, error) {
	return a.runOrthancStudyCFindWithRefresh(ctx, node, filters, true)
}

func (a *App) runOrthancStudyCFindWithRefresh(ctx context.Context, node PACSNodeConfig, filters PhysicianSearchFilters, allowRefresh bool) ([]qidoResponseItem, error) {
	resolved := node.Resolved()
	if err := a.ensureOrthancModality(ctx, node); err != nil {
		return nil, fmt.Errorf("orthanc modality ensure failed: %w", err)
	}

	queryPayload := map[string]any{
		"Level": "Study",
		"Query": map[string]string{
			"StudyInstanceUID":              "",
			"StudyDate":                     "",
			"StudyDescription":              "",
			"ModalitiesInStudy":             "",
			"PatientName":                   "",
			"PatientID":                     "",
			"PatientBirthDate":              "",
			"PatientSex":                    "",
			"AccessionNumber":               "",
			"NumberOfStudyRelatedInstances": "",
		},
		"Timeout": 60,
	}

	queryTags := queryPayload["Query"].(map[string]string)
	if strings.TrimSpace(filters.PatientID) != "" {
		queryTags["PatientID"] = strings.TrimSpace(filters.PatientID)
	}
	if strings.TrimSpace(filters.PatientNameRaw) != "" {
		queryTags["PatientName"] = strings.TrimSpace(filters.PatientNameRaw)
	} else if strings.TrimSpace(filters.PatientName) != "" {
		queryTags["PatientName"] = buildPatientSurnameCFindQuery(filters.PatientName)
	}
	if birthDate := formatDICOMDate(filters.BirthDate); birthDate != "" {
		queryTags["PatientBirthDate"] = birthDate
	}
	if sex := normalizeRemoteSex(filters.Sex); sex != "" {
		queryTags["PatientSex"] = sex
	}
	if strings.TrimSpace(filters.Modality) != "" {
		queryTags["ModalitiesInStudy"] = strings.TrimSpace(filters.Modality)
	}
	if studyDate := buildCFindStudyDate(filters.DateFrom, filters.DateTo); studyDate != "" {
		queryTags["StudyDate"] = studyDate
	}

	body, err := json.Marshal(queryPayload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(a.cfg.OrthancURL, "/")+"/modalities/"+url.PathEscape(resolved.ID)+"/query", strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	a.applyOrthancInternalRequestAuth(req)

	res, err := a.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		errorBody, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		bodyText := strings.TrimSpace(string(errorBody))
		if allowRefresh && orthancModalityMissing(res.StatusCode, bodyText) {
			a.invalidateOrthancModality(node.ID)
			return a.runOrthancStudyCFindWithRefresh(ctx, node, filters, false)
		}
		return nil, fmt.Errorf("orthanc c-find bad status %d: %s", res.StatusCode, bodyText)
	}

	queryID, err := decodeOrthancQueryID(res.Body)
	if err != nil {
		return nil, err
	}

	answerIDs, err := a.fetchOrthancQueryAnswerIDs(ctx, queryID)
	if err != nil {
		return nil, err
	}

	results := make([]qidoResponseItem, 0, len(answerIDs))
	for _, answerID := range answerIDs {
		item, err := a.fetchOrthancQueryAnswerContent(ctx, queryID, answerID)
		if err != nil {
			return nil, err
		}
		results = append(results, item)
	}

	if len(results) == 0 && shouldRetryCFindWithoutDate(filters) {
		fallbackFilters := filters
		fallbackFilters.DateFrom = ""
		fallbackFilters.DateTo = ""
		return a.runOrthancStudyCFindWithoutDate(ctx, node, fallbackFilters)
	}

	return results, nil
}

func (a *App) runOrthancStudyCFindWithoutDate(ctx context.Context, node PACSNodeConfig, filters PhysicianSearchFilters) ([]qidoResponseItem, error) {
	resolved := node.Resolved()
	a.log("warn", "physician_cfind_retry_without_date", map[string]any{
		"node_id":      resolved.ID,
		"patient_id":   filters.PatientID,
		"patient_name": filters.PatientName,
		"modality":     filters.Modality,
	})
	return a.runOrthancStudyCFindWithRefresh(ctx, node, filters, false)
}

func decodeOrthancQueryID(r io.Reader) (string, error) {
	if r == nil {
		return "", errors.New("empty orthanc c-find response")
	}

	var objectPayload struct {
		ID string `json:"ID"`
	}
	if err := json.NewDecoder(r).Decode(&objectPayload); err == nil && strings.TrimSpace(objectPayload.ID) != "" {
		return strings.TrimSpace(objectPayload.ID), nil
	}

	return "", errors.New("orthanc c-find did not return query id")
}

func (a *App) fetchOrthancQueryAnswerIDs(ctx context.Context, queryID string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(a.cfg.OrthancURL, "/")+"/queries/"+url.PathEscape(queryID)+"/answers", nil)
	if err != nil {
		return nil, err
	}
	a.applyOrthancInternalRequestAuth(req)

	res, err := a.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return nil, fmt.Errorf("orthanc query answers bad status %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}

	var stringIDs []string
	if err := json.NewDecoder(res.Body).Decode(&stringIDs); err == nil {
		return uniqueTrimmedStrings(stringIDs), nil
	}

	req2, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(a.cfg.OrthancURL, "/")+"/queries/"+url.PathEscape(queryID)+"/answers", nil)
	if err != nil {
		return nil, err
	}
	a.applyOrthancInternalRequestAuth(req2)
	res2, err := a.orthancSearchClient.Do(req2)
	if err != nil {
		return nil, err
	}
	defer res2.Body.Close()

	var intIDs []int
	if err := json.NewDecoder(res2.Body).Decode(&intIDs); err != nil {
		return nil, fmt.Errorf("decode orthanc query answers: %w", err)
	}

	result := make([]string, 0, len(intIDs))
	for _, id := range intIDs {
		result = append(result, strconv.Itoa(id))
	}
	return result, nil
}

func (a *App) fetchOrthancQueryAnswerContent(ctx context.Context, queryID, answerID string) (qidoResponseItem, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(a.cfg.OrthancURL, "/")+"/queries/"+url.PathEscape(queryID)+"/answers/"+url.PathEscape(answerID)+"/content", nil)
	if err != nil {
		return nil, err
	}
	a.applyOrthancInternalRequestAuth(req)

	res, err := a.orthancSearchClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return nil, fmt.Errorf("orthanc query answer content bad status %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}

	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read orthanc query answer content: %w", err)
	}

	item, err := decodeOrthancQueryAnswerItem(body)
	if err != nil {
		return nil, fmt.Errorf("decode orthanc query answer content: %w", err)
	}
	return item, nil
}

func decodeOrthancQueryAnswerItem(body []byte) (qidoResponseItem, error) {
	var item qidoResponseItem
	if err := json.Unmarshal(body, &item); err == nil {
		return item, nil
	}

	var flat map[string]any
	if err := json.Unmarshal(body, &flat); err != nil {
		return nil, err
	}

	normalized := make(qidoResponseItem)
	for _, mapping := range orthancQueryAnswerTagMappings {
		assignOrthancQueryAnswerValue(normalized, mapping.tag, flat, mapping.keys...)
	}
	return normalized, nil
}

func assignOrthancQueryAnswerValue(target qidoResponseItem, tag string, flat map[string]any, keys ...string) {
	if target == nil || flat == nil || target[tag].Value != nil {
		return
	}
	value, ok := orthancQueryAnswerValue(flat, keys...)
	if !ok || value == nil {
		return
	}

	switch typed := value.(type) {
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return
		}
		target[tag] = dicomJSONAttribute{Value: []json.RawMessage{mustMarshalJSON(trimmed)}}
	case float64:
		target[tag] = dicomJSONAttribute{Value: []json.RawMessage{mustMarshalJSON(strconv.Itoa(int(typed)))}}
	case []any:
		values := make([]json.RawMessage, 0, len(typed))
		for _, entry := range typed {
			text := strings.TrimSpace(fmt.Sprint(entry))
			if text == "" {
				continue
			}
			values = append(values, mustMarshalJSON(text))
		}
		if len(values) > 0 {
			target[tag] = dicomJSONAttribute{Value: values}
		}
	case map[string]any:
		assignOrthancQueryAnswerNestedValue(target, tag, typed)
	}
}

func orthancQueryAnswerValue(flat map[string]any, keys ...string) (any, bool) {
	if flat == nil {
		return nil, false
	}
	for _, key := range keys {
		if value, ok := flat[key]; ok {
			return value, true
		}
		if value, ok := flat[strings.ToLower(key)]; ok {
			return value, true
		}
		if value, ok := flat[strings.ToUpper(key)]; ok {
			return value, true
		}
	}
	return nil, false
}

func assignOrthancQueryAnswerNestedValue(target qidoResponseItem, tag string, payload map[string]any) {
	if target == nil || payload == nil || target[tag].Value != nil {
		return
	}
	rawValue, ok := payload["Value"]
	if !ok || rawValue == nil {
		return
	}
	switch typed := rawValue.(type) {
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return
		}
		target[tag] = dicomJSONAttribute{Value: []json.RawMessage{mustMarshalJSON(trimmed)}}
	case float64:
		target[tag] = dicomJSONAttribute{Value: []json.RawMessage{mustMarshalJSON(strconv.Itoa(int(typed)))}}
	case []any:
		values := make([]json.RawMessage, 0, len(typed))
		for _, entry := range typed {
			switch item := entry.(type) {
			case map[string]any:
				if alphabetic, ok := item["Alphabetic"].(string); ok && strings.TrimSpace(alphabetic) != "" {
					values = append(values, mustMarshalJSON(strings.TrimSpace(alphabetic)))
					continue
				}
				text := strings.TrimSpace(fmt.Sprint(item))
				if text != "" {
					values = append(values, mustMarshalJSON(text))
				}
			default:
				text := strings.TrimSpace(fmt.Sprint(entry))
				if text != "" {
					values = append(values, mustMarshalJSON(text))
				}
			}
		}
		if len(values) > 0 {
			target[tag] = dicomJSONAttribute{Value: values}
		}
	}
}

func (a *App) startOrthancCGetWithRefresh(ctx context.Context, node PACSNodeConfig, studyInstanceUID string, allowRefresh bool) (string, error) {
	return a.startOrthancCGetResources(ctx, node, "Study", []map[string]string{
		{"StudyInstanceUID": studyInstanceUID},
	}, allowRefresh)
}

// startOrthancCGetSeries issues a C-GET for a specific set of series of a study,
// used to remediate a partial retrieve without re-pulling series already stored.
func (a *App) startOrthancCGetSeries(ctx context.Context, node PACSNodeConfig, studyInstanceUID string, seriesInstanceUIDs []string) (string, error) {
	resources := make([]map[string]string, 0, len(seriesInstanceUIDs))
	for _, seriesUID := range seriesInstanceUIDs {
		seriesUID = strings.TrimSpace(seriesUID)
		if seriesUID == "" {
			continue
		}
		resources = append(resources, map[string]string{
			"StudyInstanceUID":  studyInstanceUID,
			"SeriesInstanceUID": seriesUID,
		})
	}
	if len(resources) == 0 {
		return "", errors.New("no series to retrieve")
	}
	return a.startOrthancCGetResources(ctx, node, "Series", resources, true)
}

func (a *App) startOrthancCGetResources(ctx context.Context, node PACSNodeConfig, level string, resources []map[string]string, allowRefresh bool) (string, error) {
	resolved := node.Resolved()
	payload, err := json.Marshal(map[string]any{
		"Asynchronous": true,
		"Level":        level,
		"Resources":    resources,
		"Timeout":      60,
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(a.cfg.OrthancURL, "/")+"/modalities/"+url.PathEscape(resolved.ID)+"/get", strings.NewReader(string(payload)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	a.applyOrthancInternalRequestAuth(req)

	orthancRetrieveClient := &http.Client{}
	res, err := orthancRetrieveClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		bodyText := strings.TrimSpace(string(body))
		if allowRefresh && orthancModalityMissing(res.StatusCode, bodyText) {
			a.invalidateOrthancModality(node.ID)
			if err := a.ensureOrthancModality(ctx, node); err != nil {
				return "", fmt.Errorf("orthanc c-get modality refresh failed: %w", err)
			}
			return a.startOrthancCGetResources(ctx, node, level, resources, false)
		}
		return "", fmt.Errorf("orthanc c-get bad status %d: %s", res.StatusCode, bodyText)
	}

	var payloadResponse struct {
		ID string `json:"ID"`
	}
	if err := json.NewDecoder(res.Body).Decode(&payloadResponse); err != nil {
		return "", fmt.Errorf("decode orthanc c-get response: %w", err)
	}
	if strings.TrimSpace(payloadResponse.ID) == "" {
		return "", errors.New("orthanc c-get did not return job id")
	}
	return payloadResponse.ID, nil
}

func (a *App) waitForStudyInOrthanc(ctx context.Context, studyUID string, pollInterval, timeout time.Duration) (bool, string, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		available, orthancStudyID, err := a.findOrthancStudy(ctx, studyUID)
		if err != nil {
			return false, "", err
		}
		if available {
			return true, orthancStudyID, nil
		}
		select {
		case <-ctx.Done():
			return false, "", ctx.Err()
		case <-time.After(pollInterval):
		}
	}
	return false, "", nil
}

func buildStoneViewerURL(studyInstanceUID string) string {
	return "/stone-webviewer/index.html?study=" + url.QueryEscape(strings.TrimSpace(studyInstanceUID)) + "&menu=hidden"
}

func buildOHIFViewerURL(studyInstanceUID string) string {
	return "/ohif/viewer?StudyInstanceUIDs=" + url.QueryEscape(strings.TrimSpace(studyInstanceUID))
}
