package main

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

type PhysicianResultsResponse struct {
	Physician      PhysicianSummary       `json:"physician"`
	Filters        PhysicianSearchFilters `json:"filters"`
	Results        []PhysicianResult      `json:"results"`
	CanShare       bool                   `json:"can_share"`
	CanViewMetrics bool                   `json:"can_view_metrics"`
}

type PhysicianLoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type PhysicianLoginResponse struct {
	Status         string           `json:"status"`
	Message        string           `json:"message"`
	Physician      PhysicianSummary `json:"physician,omitempty"`
	SessionToken   string           `json:"session_token,omitempty"`
	CanShare       bool             `json:"can_share"`
	CanViewMetrics bool             `json:"can_view_metrics"`
}

type PhysicianResultsErrorResponse struct {
	Message string `json:"message"`
}

type PhysicianRetrieveRequest struct {
	Username         string `json:"username"`
	StudyInstanceUID string `json:"study_instance_uid"`
	// SourceNodeID is the PACS node the study was found on. The UI already knows
	// the active search source, so it sends it here as the authoritative origin.
	// When empty/invalid the backend falls back to inferring it from the
	// physician's recent queries / cached studies.
	SourceNodeID string `json:"source_node_id,omitempty"`
	// Modality is the study's modality as known by the UI search results. At
	// retrieve time the study is not local yet, so the UI is the source for the
	// usage "retrieves by modality" breakdown.
	Modality string `json:"modality,omitempty"`
}

type PhysicianRetrieveResponse struct {
	JobID            string `json:"job_id"`
	StudyInstanceUID string `json:"study_instance_uid"`
	Status           string `json:"status"`
	ViewerURL        string `json:"viewer_url,omitempty"`
	OHIFViewerURL    string `json:"ohif_viewer_url,omitempty"`
}

type physicianSessionSnapshot struct {
	SessionID   string
	PhysicianID string
	ExpiresAt   time.Time
	Status      string
}

type PhysicianSummary struct {
	ID            string `json:"id"`
	Username      string `json:"username"`
	DNI           string `json:"dni"`
	FullName      string `json:"full_name"`
	LicenseNumber string `json:"license_number"`
}

type PhysicianSearchFilters struct {
	PatientID      string `json:"patient_id,omitempty"`
	PatientName    string `json:"patient_name,omitempty"`
	PatientNameRaw string `json:"patient_name_raw,omitempty"`
	BirthDate      string `json:"birth_date,omitempty"`
	Sex            string `json:"sex,omitempty"`
	DateFrom       string `json:"date_from,omitempty"`
	DateTo         string `json:"date_to,omitempty"`
	Modality       string `json:"modality,omitempty"`
	Source         string `json:"source,omitempty"`
}

type PhysicianResult struct {
	StudyInstanceUID    string   `json:"study_instance_uid"`
	PatientName         string   `json:"patient_name"`
	PatientID           string   `json:"patient_id"`
	StudyDate           string   `json:"study_date"`
	StudyDescription    string   `json:"study_description"`
	NumberOfImages      int      `json:"number_of_images,omitempty"`
	Modalities          []string `json:"modalities"`
	Locations           []string `json:"locations"`
	SourceNodeID        string   `json:"source_node_id,omitempty"`
	AndesPrestacionID   string   `json:"andes_prestacion_id,omitempty"`
	AndesPrestacion     string   `json:"andes_prestacion,omitempty"`
	AndesProfessional   string   `json:"andes_professional,omitempty"`
	CacheStatus         string   `json:"cache_status"`
	RetrieveStatus      string   `json:"retrieve_status"`
	RetrievePhase       string   `json:"retrieve_phase,omitempty"`
	RetrieveProgress    int      `json:"retrieve_progress,omitempty"`
	PartialFilter       bool     `json:"partial_filter"`
	ViewerURL           string   `json:"viewer_url,omitempty"`
	OHIFViewerURL       string   `json:"ohif_viewer_url,omitempty"`
	DownloadURL         string   `json:"download_url,omitempty"`
	HIS                 bool     `json:"his"`
	SourceNodeAvailable bool     `json:"source_node_available"`
}

func clonePhysicianResults(results []PhysicianResult) []PhysicianResult {
	cloned := make([]PhysicianResult, 0, len(results))
	for _, result := range results {
		item := result
		item.Modalities = append([]string(nil), result.Modalities...)
		item.Locations = append([]string(nil), result.Locations...)
		cloned = append(cloned, item)
	}
	return cloned
}

func (a *App) writePhysicianLoginResponse(w http.ResponseWriter, statusCode int, status, message string, physician PhysicianSummary) {
	payload := PhysicianLoginResponse{
		Status:  strings.TrimSpace(status),
		Message: strings.TrimSpace(message),
	}
	if strings.TrimSpace(physician.ID) != "" {
		payload.Physician = physician
		payload.CanShare = a.physicianCanShare(physician)
		payload.CanViewMetrics = a.physicianCanViewMetrics(physician)
	}
	writeJSON(w, statusCode, payload)
}

func (a *App) handlePhysicianStudyRoute(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasSuffix(r.URL.Path, "/access"):
		a.action(ActionViewerAccessGrant, a.handlePhysicianStudyAccess)(w, r)
	case strings.HasSuffix(r.URL.Path, "/preview"):
		a.handlePhysicianStudyPreview(w, r)
	case strings.HasSuffix(r.URL.Path, "/share"):
		a.action(ActionShareLinkCreate, a.handlePhysicianStudyShare)(w, r)
	default:
		http.NotFound(w, r)
	}
}

// physicianCanShare reports whether the physician's resolved RBAC role grants
// the share_link:create permission (e.g. listed in rbac.physician_sharers).
func (a *App) physicianCanShare(summary PhysicianSummary) bool {
	if a.rbac == nil {
		return false
	}
	return a.rbac.hasPermission(a.rbac.roleForPhysician(summary), actionCatalog[ActionShareLinkCreate].Permission)
}

func (a *App) handlePhysicianStudyShare(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	studyUID := studyUIDFromActionPath(r.URL.Path, "/api/physician/studies/", "/share")
	if studyUID == "" {
		http.NotFound(w, r)
		return
	}

	var reqBody PatientStudyShareRequest
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil && !errors.Is(err, io.EOF) {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}

	viewerKind := normalizeViewerKind(reqBody.ViewerKind)
	if viewerKind == "" {
		viewerKind = "stone"
	}
	channel := normalizeStudyShareChannel(reqBody.Channel)
	if channel == "" {
		http.Error(w, "invalid share channel", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	// The action() decorator already resolved + authorized the physician actor
	// (share_link:create). Fall back to resolving the session directly if the
	// handler is ever called outside the decorator.
	physicianID := ""
	if actor, ok := actorFromContext(ctx); ok && actor.Kind == SubjectPhysician {
		physicianID = actor.ID
	} else {
		_, physician, err := a.requirePhysicianSessionSummary(ctx, r)
		if err != nil {
			http.Error(w, "invalid session", http.StatusUnauthorized)
			return
		}
		physicianID = physician.ID
	}

	setActionDim(ctx, "study_uid", studyUID)
	setActionDim(ctx, "channel", channel)
	setActionDim(ctx, "viewer_kind", viewerKind)

	// Physician share links can only point at studies already cached locally
	// (i.e. previously retrieved), mirroring the physician viewer access rule.
	isLocal, _, err := a.findOrthancStudy(ctx, studyUID)
	if err != nil {
		http.Error(w, "failed to validate physician study access", http.StatusInternalServerError)
		return
	}
	if !isLocal {
		http.Error(w, "study not available for physician share", http.StatusNotFound)
		return
	}

	// Physician shares are not owned by a patient row; persist with a NULL owner.
	shareURL, rawToken, expiresAt, maxUses, err := a.createStudyShareLink(
		ctx,
		"",
		studyUID,
		viewerKind,
		channel,
		reqBody,
		r,
		time.Time{},
	)
	if err != nil {
		http.Error(w, "failed to create share link", http.StatusInternalServerError)
		return
	}

	shareMessage := fmt.Sprintf("Te comparto un estudio de diagnóstico por imágenes. Está disponible hasta el %s.", expiresAt.In(time.UTC).Format("2006-01-02 15:04 UTC"))
	qrCodeDataURL, err := buildStudyShareQRCodeDataURL(shareURL)
	if err != nil {
		http.Error(w, "failed to build share qr", http.StatusInternalServerError)
		return
	}
	whatsAppURL := "https://wa.me/?text=" + url.QueryEscape(shareMessage+" "+shareURL)
	mailSubject := "Estudio por imágenes compartido | Salud Pública Neuquén"
	mailBody := shareMessage + "\n\n" + shareURL
	mailToURL := "mailto:?subject=" + url.QueryEscape(mailSubject) + "&body=" + url.QueryEscape(mailBody)

	a.log("info", "physician_study_share_created", map[string]any{
		"physician_id":       physicianID,
		"study_instance_uid": studyUID,
		"viewer_kind":        viewerKind,
		"channel":            channel,
		"expires_at":         expiresAt.UTC().Format(time.RFC3339),
		"max_uses":           maxUses,
		"client_ip":          clientIPForRateLimit(r),
	})

	_ = rawToken
	writeJSON(w, http.StatusOK, PatientStudyShareResponse{
		Status:           "ok",
		StudyInstanceUID: studyUID,
		ViewerKind:       viewerKind,
		ShareURL:         shareURL,
		QRCodeDataURL:    qrCodeDataURL,
		WhatsAppURL:      whatsAppURL,
		MailToURL:        mailToURL,
		ExpiresAt:        expiresAt.UTC().Format(time.RFC3339),
		MaxUses:          maxUses,
	})
}

func (a *App) handlePhysicianStudyPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	studyUID := studyUIDFromActionPath(r.URL.Path, "/api/physician/studies/", "/preview")
	if studyUID == "" {
		http.NotFound(w, r)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	if _, _, err := a.requirePhysicianSessionSummary(ctx, r); err != nil {
		http.Error(w, "invalid session", http.StatusUnauthorized)
		return
	}

	isLocal, _, err := a.findOrthancStudy(ctx, studyUID)
	if err != nil {
		http.Error(w, "failed to validate physician study access", http.StatusInternalServerError)
		return
	}
	if !isLocal {
		http.Error(w, "study not available for physician preview", http.StatusNotFound)
		return
	}

	previewLimit := a.previewImageLimit()
	items, totalAvailable, err := a.listStudyPreviewItems(ctx, studyUID, previewLimit)
	if err != nil {
		http.Error(w, "failed to load study preview", http.StatusBadGateway)
		return
	}
	if len(items) == 0 {
		http.Error(w, "no preview images available", http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, PatientStudyPreviewResponse{
		Status:           "ok",
		StudyInstanceUID: studyUID,
		Items:            items,
		TotalShown:       len(items),
		Limit:            previewLimit,
		TotalAvailable:   totalAvailable,
		Truncated:        totalAvailable > len(items),
	})
}

func (a *App) handlePhysicianStudyAccess(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	studyUID := studyUIDFromAccessPath(r.URL.Path, "/api/physician/studies/")
	if studyUID == "" {
		http.NotFound(w, r)
		return
	}
	viewerKind := normalizeViewerKind(r.URL.Query().Get("viewer"))
	if viewerKind == "" {
		http.Error(w, "invalid viewer", http.StatusBadRequest)
		return
	}
	setActionDim(r.Context(), "study_uid", studyUID)
	setActionDim(r.Context(), "viewer_kind", viewerKind)
	rawSessionToken := sessionCookieToken(r, physicianSessionCookieName)
	if rawSessionToken == "" {
		http.Error(w, "missing session token", http.StatusUnauthorized)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	session, err := a.physicianSessionByToken(ctx, rawSessionToken)
	if err != nil {
		http.Error(w, "invalid session", http.StatusUnauthorized)
		return
	}

	isLocal, _, err := a.findOrthancStudy(ctx, studyUID)
	if err != nil {
		http.Error(w, "failed to validate physician study access", http.StatusInternalServerError)
		return
	}
	if !isLocal {
		http.Error(w, "study not available for physician viewer", http.StatusNotFound)
		return
	}

	accessURL, expiresAt, err := a.createViewerAccessGrant(ctx, "physician", "", session.SessionID, studyUID, viewerKind, r, session.ExpiresAt)
	if err != nil {
		http.Error(w, "failed to create viewer access grant", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, ViewerAccessGrantResponse{
		Status:           "ok",
		URL:              accessURL,
		StudyInstanceUID: studyUID,
		ViewerKind:       viewerKind,
		ExpiresAt:        expiresAt.UTC().Format(time.RFC3339),
	})
}

func (a *App) handlePhysicianResults(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	username := normalizeProfessionalDocumentInput(r.URL.Query().Get("username"))

	filters := PhysicianSearchFilters{
		PatientID:   strings.TrimSpace(r.URL.Query().Get("patient_id")),
		PatientName: strings.TrimSpace(r.URL.Query().Get("patient_name")),
		DateFrom:    strings.TrimSpace(r.URL.Query().Get("date_from")),
		DateTo:      strings.TrimSpace(r.URL.Query().Get("date_to")),
		Modality:    strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("modality"))),
		Source:      normalizePhysicianSearchSource(r.URL.Query().Get("source")),
	}
	useInitialCachePeriod := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("use_initial_cache_period")), "true")
	if filters.Source != physicianSearchSourceLocalCache && !hasPhysicianQueryFilters(filters) {
		writeJSON(w, http.StatusBadRequest, PhysicianResultsErrorResponse{Message: "Seleccione al menos un filtro adicional antes de consultar un PACS remoto."})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	_, physician, err := a.requirePhysicianSessionSummary(ctx, r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, PhysicianResultsErrorResponse{Message: "Sesión profesional inválida."})
		return
	}
	if username != "" && subtle.ConstantTimeCompare([]byte(username), []byte(physician.Username)) != 1 {
		writeJSON(w, http.StatusForbidden, PhysicianResultsErrorResponse{Message: "La sesión profesional no coincide con el usuario solicitado."})
		return
	}
	results, err := a.listPhysicianResults(ctx, physician.ID, filters, useInitialCachePeriod)
	if err != nil {
		a.log("error", "physician_results_query_failed", map[string]any{
			"username":     physician.Username,
			"physician_id": physician.ID,
			"error":        err.Error(),
		})
		writeJSON(w, http.StatusInternalServerError, PhysicianResultsErrorResponse{Message: "No se pudieron consultar los resultados del profesional."})
		return
	}

	resp := PhysicianResultsResponse{
		Physician:      physician,
		Filters:        filters,
		Results:        results,
		CanShare:       a.physicianCanShare(physician),
		CanViewMetrics: a.physicianCanViewMetrics(physician),
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (a *App) handlePhysicianDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	username := normalizeProfessionalDocumentInput(r.URL.Query().Get("username"))
	studyInstanceUID := strings.TrimSpace(r.URL.Query().Get("study_instance_uid"))
	if studyInstanceUID == "" {
		http.Error(w, "missing required query params", http.StatusBadRequest)
		return
	}
	setActionDim(r.Context(), "study_uid", studyInstanceUID)

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	_, physician, err := a.requirePhysicianSessionSummary(ctx, r)
	if err != nil {
		http.Error(w, "invalid session", http.StatusUnauthorized)
		return
	}
	if username != "" && subtle.ConstantTimeCompare([]byte(username), []byte(physician.Username)) != 1 {
		http.Error(w, "physician session does not match requested user", http.StatusForbidden)
		return
	}
	isLocal, _, err := a.findOrthancStudy(ctx, studyInstanceUID)
	if err != nil {
		http.Error(w, "failed to validate physician study", http.StatusInternalServerError)
		return
	}
	if !isLocal {
		http.Error(w, "study not available for physician download", http.StatusNotFound)
		return
	}

	if mods, mErr := a.physicianStudyModalities(ctx, physician.ID, studyInstanceUID); mErr == nil {
		setActionDim(r.Context(), "modality", usageModalityDim(mods))
	}

	usedDownloads, weeklyLimit, allowed, err := a.enforcePhysicianDownloadLimit(ctx, physician.ID, studyInstanceUID)
	if err != nil {
		a.log("error", "physician_download_limit_check_failed", map[string]any{
			"physician_id":       physician.ID,
			"study_instance_uid": studyInstanceUID,
			"error":              err.Error(),
		})
		http.Error(w, "failed to validate physician download limit", http.StatusInternalServerError)
		return
	}
	if !allowed {
		a.log("info", "physician_download_limit_reached", map[string]any{
			"physician_id":       physician.ID,
			"weekly_limit":       weeklyLimit,
			"downloads_used":     usedDownloads,
			"study_instance_uid": studyInstanceUID,
		})
		http.Error(w, "weekly physician download limit reached", http.StatusTooManyRequests)
		return
	}

	if err := a.streamStudyArchiveByUID(ctx, w, studyInstanceUID); err != nil {
		a.log("error", "physician_download_failed", map[string]any{
			"physician_id":       physician.ID,
			"study_instance_uid": studyInstanceUID,
			"error":              err.Error(),
		})
		http.Error(w, "failed to download study archive", http.StatusBadGateway)
		return
	}

	a.log("info", "physician_download_completed", map[string]any{
		"physician_id":       physician.ID,
		"study_instance_uid": studyInstanceUID,
		"weekly_limit":       weeklyLimit,
		"downloads_used":     usedDownloads,
	})
}

func (a *App) handlePhysicianLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var reqBody PhysicianLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		a.writePhysicianLoginResponse(w, http.StatusBadRequest, "invalid_request", "JSON inválido.", PhysicianSummary{})
		return
	}

	reqBody.Username = normalizeProfessionalDocumentInput(reqBody.Username)
	if reqBody.Username == "" {
		a.writePhysicianLoginResponse(w, http.StatusBadRequest, "invalid_request", "username es requerido.", PhysicianSummary{})
		return
	}
	setActionDim(r.Context(), "identifier", reqBody.Username)
	if !a.enforceLoginRateLimit(w, r, physicianLoginRateLimitPolicy(), reqBody.Username) {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	if a.externalConfig != nil && !a.externalConfig.Professional.FakeAuth {
		if err := authenticateProfessionalLDAP(ctx, reqBody.Username, reqBody.Password); err != nil {
			if errors.Is(err, ErrProfessionalInvalidCredentials) {
				a.writePhysicianLoginResponse(w, http.StatusUnauthorized, "invalid_credentials", "Usuario o contraseña inválidos.", PhysicianSummary{})
				return
			}
			a.log("error", "physician_ldap_auth_failed", map[string]any{
				"username": reqBody.Username,
				"error":    err.Error(),
			})
			a.writePhysicianLoginResponse(w, http.StatusBadGateway, "provider_unavailable", "No se pudo validar la autenticación institucional.", PhysicianSummary{})
			return
		}
	}

	physician, err := a.ensurePhysicianRecord(ctx, reqBody.Username)
	if err != nil {
		if errors.Is(err, ErrProfessionalIdentityNotFound) {
			a.writePhysicianLoginResponse(w, http.StatusNotFound, "professional_not_found", "Profesional no registrado.", PhysicianSummary{})
			return
		}
		if errors.Is(err, ErrProfessionalNotLicensed) {
			a.writePhysicianLoginResponse(w, http.StatusForbidden, "professional_not_licensed", "El profesional no se encuentra matriculado.", PhysicianSummary{})
			return
		}
		a.writePhysicianLoginResponse(w, http.StatusBadGateway, "provider_unavailable", "No se pudo validar el acceso profesional.", PhysicianSummary{})
		return
	}

	_, rawSessionToken, expiresAt, err := a.createPhysicianSession(ctx, physician.ID, r)
	if err != nil {
		a.writePhysicianLoginResponse(w, http.StatusInternalServerError, "internal_error", "No se pudo crear la sesión profesional.", PhysicianSummary{})
		return
	}
	setPortalSessionCookie(w, r, physicianSessionCookieName, rawSessionToken, expiresAt)

	a.writePhysicianLoginResponse(w, http.StatusOK, "ready", "Ingreso profesional validado.", physician)
}

func (a *App) handlePhysicianLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	rawToken := sessionCookieToken(r, physicianSessionCookieName)
	if rawToken != "" {
		if err := a.invalidatePhysicianSessionByToken(ctx, rawToken); err != nil && !errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "failed to logout physician session", http.StatusInternalServerError)
			return
		}
	}

	clearPortalSessionCookie(w, r, physicianSessionCookieName)
	clearViewerGrantCookie(w, r)
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "logged_out",
	})
}

const physicianSearchSourceLocalCache = "local_cache"

func normalizePhysicianSearchSource(value string) string {
	source := strings.ToLower(strings.TrimSpace(value))
	if source == "" {
		return physicianSearchSourceLocalCache
	}
	return source
}

func hasPhysicianQueryFilters(filters PhysicianSearchFilters) bool {
	return strings.TrimSpace(filters.PatientID) != "" ||
		strings.TrimSpace(filters.PatientName) != "" ||
		strings.TrimSpace(filters.DateFrom) != "" ||
		strings.TrimSpace(filters.DateTo) != "" ||
		strings.TrimSpace(filters.Modality) != ""
}

func (a *App) handlePhysicianRetrieve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var reqBody PhysicianRetrieveRequest
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}

	reqBody.Username = normalizeProfessionalDocumentInput(reqBody.Username)
	reqBody.StudyInstanceUID = strings.TrimSpace(reqBody.StudyInstanceUID)
	if reqBody.StudyInstanceUID == "" {
		http.Error(w, "study_instance_uid is required", http.StatusBadRequest)
		return
	}
	setActionDim(r.Context(), "study_uid", reqBody.StudyInstanceUID)

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	_, physician, err := a.requirePhysicianSessionSummary(ctx, r)
	if err != nil {
		http.Error(w, "invalid session", http.StatusUnauthorized)
		return
	}
	if reqBody.Username != "" && subtle.ConstantTimeCompare([]byte(reqBody.Username), []byte(physician.Username)) != 1 {
		http.Error(w, "physician session does not match requested user", http.StatusForbidden)
		return
	}

	physicianDNI := strings.TrimSpace(physician.Username)
	if physicianDNI == "" {
		physicianDNI = strings.TrimSpace(physician.DNI)
	}
	setActionDim(r.Context(), "physician_dni", physicianDNI)
	setActionDim(r.Context(), "modality", usageModalityDim(strings.Split(reqBody.Modality, "/")))

	resp, err := a.queuePhysicianRetrieve(ctx, physician, reqBody.StudyInstanceUID, reqBody.SourceNodeID)
	if err != nil {
		statusCode := http.StatusBadGateway
		if errors.Is(err, ErrSourceNodeUnavailable) {
			statusCode = http.StatusServiceUnavailable
		}
		a.log("error", "physician_retrieve_failed", map[string]any{
			"username":           physician.Username,
			"physician_id":       physician.ID,
			"study_instance_uid": reqBody.StudyInstanceUID,
			"error":              err.Error(),
		})
		http.Error(w, err.Error(), statusCode)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func physicianLoginRateLimitPolicy() LoginRateLimitPolicy {
	return LoginRateLimitPolicy{
		Endpoint: "physician_login",
		Rules: []LoginRateLimitRule{
			{Scope: "ip", Limit: 20, Window: time.Minute},
			{Scope: "identifier", Limit: 5, Window: 10 * time.Minute},
		},
	}
}

func (a *App) createPhysicianSession(ctx context.Context, physicianID string, r *http.Request) (string, string, time.Time, error) {
	rawToken, err := randomToken(32)
	if err != nil {
		return "", "", time.Time{}, err
	}
	expiresAt := time.Now().UTC().Add(a.portalSessionDuration())
	var sessionID string
	err = a.db.QueryRowContext(ctx, `
		INSERT INTO physician_sessions (
			physician_id, status, auth_provider, mfa_status,
			expires_at, last_seen_at, client_ip, user_agent, token_hash
		) VALUES (
			$1::uuid, 'active', 'portal_login', 'not_required',
			$2, now(), NULLIF($3, '')::inet, NULLIF($4, ''), $5
		)
		RETURNING id::text
	`, physicianID, expiresAt, clientIPForRateLimit(r), strings.TrimSpace(r.UserAgent()), tokenHash(rawToken)).Scan(&sessionID)
	if err != nil {
		return "", "", time.Time{}, err
	}
	return sessionID, rawToken, expiresAt, nil
}

func (a *App) physicianSessionByToken(ctx context.Context, rawToken string) (physicianSessionSnapshot, error) {
	var session physicianSessionSnapshot
	err := a.db.QueryRowContext(ctx, `
		SELECT id::text, physician_id::text, expires_at, status
		FROM physician_sessions
		WHERE token_hash = $1
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, tokenHash(rawToken)).Scan(&session.SessionID, &session.PhysicianID, &session.ExpiresAt, &session.Status)
	if err != nil {
		return physicianSessionSnapshot{}, err
	}
	if session.Status != "active" || time.Now().UTC().After(session.ExpiresAt) {
		return physicianSessionSnapshot{}, sql.ErrNoRows
	}
	_, _ = a.db.ExecContext(ctx, `UPDATE physician_sessions SET last_seen_at = now() WHERE id = $1::uuid`, session.SessionID)
	return session, nil
}

func (a *App) invalidatePhysicianSessionByToken(ctx context.Context, rawToken string) error {
	result, err := a.db.ExecContext(ctx, `
		UPDATE physician_sessions
		SET status = 'logged_out',
		    expires_at = now(),
		    last_seen_at = now()
		WHERE token_hash = $1
		  AND status = 'active'
	`, tokenHash(rawToken))
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (a *App) requirePhysicianSessionSummary(ctx context.Context, r *http.Request) (physicianSessionSnapshot, PhysicianSummary, error) {
	rawSessionToken := sessionCookieToken(r, physicianSessionCookieName)
	if rawSessionToken == "" {
		return physicianSessionSnapshot{}, PhysicianSummary{}, sql.ErrNoRows
	}

	session, err := a.physicianSessionByToken(ctx, rawSessionToken)
	if err != nil {
		return physicianSessionSnapshot{}, PhysicianSummary{}, err
	}

	physician, err := a.getPhysicianSummaryByID(ctx, session.PhysicianID)
	if err != nil {
		return physicianSessionSnapshot{}, PhysicianSummary{}, err
	}

	return session, physician, nil
}

func (a *App) queuePhysicianRetrieve(ctx context.Context, physician PhysicianSummary, studyInstanceUID, preferredSourceNodeID string) (PhysicianRetrieveResponse, error) {
	activeJob, err := a.findActiveRetrieveJobByStudy(ctx, studyInstanceUID)
	if err != nil {
		return PhysicianRetrieveResponse{}, err
	}
	if activeJob != nil {
		return PhysicianRetrieveResponse{
			JobID:            activeJob.JobID,
			StudyInstanceUID: activeJob.StudyInstanceUID,
			Status:           activeJob.Status,
		}, nil
	}

	sourceNodeID, err := a.resolvePhysicianRetrieveSourceNode(ctx, physician.ID, studyInstanceUID, preferredSourceNodeID)
	if err != nil {
		return PhysicianRetrieveResponse{}, err
	}
	if !a.sourceNodeAvailable(sourceNodeID) {
		return PhysicianRetrieveResponse{}, fmt.Errorf("%w: %s", ErrSourceNodeUnavailable, sourceNodeID)
	}
	modalities, err := a.physicianStudyModalities(ctx, physician.ID, studyInstanceUID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return PhysicianRetrieveResponse{}, err
	}
	if blocked := a.blockedRetrieveModality(modalities); blocked != "" {
		return PhysicianRetrieveResponse{}, fmt.Errorf("retrieve blocked for modality %s", blocked)
	}

	jobID, err := a.insertRetrieveJob(ctx, studyInstanceUID, sourceNodeID, "physician", physician.ID)
	if err != nil {
		return PhysicianRetrieveResponse{}, fmt.Errorf("insert retrieve job: %w", err)
	}
	a.log("info", "physician_retrieve_queued", map[string]any{
		"physician_id":       physician.ID,
		"study_instance_uid": studyInstanceUID,
		"source_node_id":     sourceNodeID,
		"job_id":             jobID,
	})
	a.enqueueRetrieveJob(jobID)

	return PhysicianRetrieveResponse{
		JobID:            jobID,
		StudyInstanceUID: studyInstanceUID,
		Status:           "queued",
	}, nil
}

func (a *App) physicianStudyModalities(ctx context.Context, physicianID, studyInstanceUID string) ([]string, error) {
	var raw []byte
	err := a.db.QueryRowContext(ctx, `
		SELECT result
		FROM physician_recent_queries prq
		CROSS JOIN LATERAL jsonb_array_elements(COALESCE(prq.query_json->'results', '[]'::jsonb)) AS result
		WHERE prq.physician_id = $1::uuid
		  AND result->>'study_instance_uid' = $2
		ORDER BY prq.searched_at DESC, prq.id DESC
		LIMIT 1
	`, physicianID, studyInstanceUID).Scan(&raw)
	if err != nil {
		return nil, err
	}

	var result struct {
		Modalities []string `json:"modalities"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &result); err != nil {
			return nil, fmt.Errorf("parse physician retrieve result json: %w", err)
		}
	}
	return result.Modalities, nil
}

func (a *App) physicianWeeklyDownloadLimit() int {
	if a.externalConfig == nil {
		return 100
	}
	if a.externalConfig.Professional.WeeklyDownloadLimit <= 0 {
		return 100
	}
	return a.externalConfig.Professional.WeeklyDownloadLimit
}

func (a *App) enforcePhysicianDownloadLimit(ctx context.Context, physicianID, studyInstanceUID string) (int, int, bool, error) {
	limit := a.physicianWeeklyDownloadLimit()
	windowStart := startOfCurrentWeek(time.Now().In(time.Local))

	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, limit, false, err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var used int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM physician_download_events
		WHERE physician_id = $1::uuid
		  AND downloaded_at >= $2
	`, physicianID, windowStart).Scan(&used); err != nil {
		return 0, limit, false, err
	}
	if used >= limit {
		return used, limit, false, nil
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO physician_download_events (
			physician_id, study_instance_uid, downloaded_at
		) VALUES (
			$1::uuid, $2, now()
		)
	`, physicianID, studyInstanceUID); err != nil {
		return 0, limit, false, err
	}

	if err := tx.Commit(); err != nil {
		return 0, limit, false, err
	}

	return used + 1, limit, true, nil
}

func (a *App) getPhysicianSourceNodeFromRecentQueries(ctx context.Context, physicianID, studyInstanceUID string) (PACSNodeConfig, string, error) {
	var (
		resultSourceNodeID string
		querySource        string
	)
	err := a.db.QueryRowContext(ctx, `
		SELECT
			COALESCE(result->>'source_node_id', ''),
			COALESCE(prq.query_json->>'source', '')
		FROM physician_recent_queries prq
		CROSS JOIN LATERAL jsonb_array_elements(COALESCE(prq.query_json->'results', '[]'::jsonb)) AS result
		WHERE prq.physician_id = $1::uuid
		  AND result->>'study_instance_uid' = $2
		ORDER BY prq.searched_at DESC, prq.id DESC
		LIMIT 1
	`, physicianID, studyInstanceUID).Scan(&resultSourceNodeID, &querySource)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return PACSNodeConfig{}, "", sql.ErrNoRows
		}
		return PACSNodeConfig{}, "", fmt.Errorf("resolve physician source from recent queries: %w", err)
	}
	sourceNodeID := strings.TrimSpace(resultSourceNodeID)
	if sourceNodeID == "" {
		sourceNodeID = normalizePhysicianSearchSource(querySource)
		if sourceNodeID == physicianSearchSourceLocalCache {
			sourceNodeID = ""
		}
	}
	if strings.TrimSpace(sourceNodeID) == "" {
		return PACSNodeConfig{}, "", sql.ErrNoRows
	}

	node, err := a.getConfiguredNode(sourceNodeID)
	if err != nil {
		return PACSNodeConfig{}, "", err
	}
	return node, sourceNodeID, nil
}

// resolvePhysicianRetrieveSourceNode picks the PACS node to retrieve from. A
// caller-provided node id (sent by the UI from the active search source) is
// authoritative as long as it matches a configured node; otherwise it falls
// back to inferring it from the physician's recent queries / cached studies.
func (a *App) resolvePhysicianRetrieveSourceNode(ctx context.Context, physicianID, studyInstanceUID, preferredSourceNodeID string) (string, error) {
	if node := a.preferredConfiguredNodeID(preferredSourceNodeID); node != "" {
		return node, nil
	}
	if strings.TrimSpace(preferredSourceNodeID) != "" {
		a.log("warn", "physician_retrieve_source_node_invalid", map[string]any{
			"physician_id":       physicianID,
			"study_instance_uid": studyInstanceUID,
			"requested_node_id":  strings.TrimSpace(preferredSourceNodeID),
		})
	}

	_, sourceNodeID, err := a.getPhysicianSourceNode(ctx, physicianID, studyInstanceUID)
	if err != nil {
		return "", err
	}
	return sourceNodeID, nil
}

// preferredConfiguredNodeID returns the trimmed node id when it matches a
// configured PACS node, otherwise "". This keeps the caller-provided origin
// authoritative only when it is a real node.
func (a *App) preferredConfiguredNodeID(nodeID string) string {
	trimmed := strings.TrimSpace(nodeID)
	if trimmed == "" {
		return ""
	}
	if _, err := a.getConfiguredNode(trimmed); err == nil {
		return trimmed
	}
	return ""
}

func (a *App) getPhysicianSourceNode(ctx context.Context, physicianID, studyInstanceUID string) (PACSNodeConfig, string, error) {
	node, sourceNodeID, err := a.getPhysicianSourceNodeFromRecentQueries(ctx, physicianID, studyInstanceUID)
	if err == nil {
		return node, sourceNodeID, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return PACSNodeConfig{}, "", err
	}

	var locationsJSONRaw []byte
	err = a.db.QueryRowContext(ctx, `
		SELECT locations_json
		FROM cached_studies
		WHERE study_instance_uid = $1
	`, studyInstanceUID).Scan(&locationsJSONRaw)
	if err == nil {
		var locations []string
		if len(locationsJSONRaw) > 0 && json.Unmarshal(locationsJSONRaw, &locations) == nil {
			for _, location := range locations {
				for _, node := range a.externalConfig.PACSNodes {
					if node.ID == location || strings.EqualFold(node.Name, location) || strings.EqualFold(node.ID, location) {
						return node, node.ID, nil
					}
				}
			}
		}
	}
	if len(a.externalConfig.PACSNodes) == 1 {
		return a.externalConfig.PACSNodes[0], a.externalConfig.PACSNodes[0].ID, nil
	}
	return PACSNodeConfig{}, "", fmt.Errorf("source node not resolved for physician study %q", studyInstanceUID)
}

func (a *App) ensurePhysicianRecord(ctx context.Context, username string) (PhysicianSummary, error) {
	var physician PhysicianSummary

	identity, err := a.professionalIdentitySource.ResolveByUsername(ctx, username)
	if err != nil {
		return PhysicianSummary{}, err
	}
	if !identity.Licensed {
		return PhysicianSummary{}, ErrProfessionalNotLicensed
	}
	dni := identity.DNI
	if dni == "" {
		dni = digitsOnly(username)
		if dni == "" {
			dni = username
		}
	}

	err = a.db.QueryRowContext(ctx, `
		INSERT INTO physicians (username, dni, full_name, license_number, licensed, auth_provider, mfa_enabled, last_login_at, last_success_auth_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, true, now(), now(), now())
		ON CONFLICT (username) DO UPDATE SET
			dni = EXCLUDED.dni,
			full_name = EXCLUDED.full_name,
			license_number = EXCLUDED.license_number,
			licensed = EXCLUDED.licensed,
			last_login_at = now(),
			last_success_auth_at = now(),
			updated_at = now()
		RETURNING id::text, username, COALESCE(dni, ''), COALESCE(full_name, ''), COALESCE(license_number, '')
	`,
		username,
		dni,
		identity.FullName,
		identity.LicenseNumber,
		identity.Licensed,
		identity.SourceSystem,
	).Scan(&physician.ID, &physician.Username, &physician.DNI, &physician.FullName, &physician.LicenseNumber)
	if err != nil {
		return PhysicianSummary{}, fmt.Errorf("upsert physician: %w", err)
	}

	return physician, nil
}

func (a *App) getPhysicianSummaryByID(ctx context.Context, physicianID string) (PhysicianSummary, error) {
	var physician PhysicianSummary
	err := a.db.QueryRowContext(ctx, `
		SELECT id::text, username, COALESCE(dni, ''), COALESCE(full_name, ''), COALESCE(license_number, '')
		FROM physicians
		WHERE id = $1::uuid
	`, physicianID).Scan(
		&physician.ID,
		&physician.Username,
		&physician.DNI,
		&physician.FullName,
		&physician.LicenseNumber,
	)
	if err != nil {
		return PhysicianSummary{}, err
	}
	return physician, nil
}

func (a *App) searchPhysicianResultsFromNode(ctx context.Context, physician PhysicianSummary, node PACSNodeConfig, filters PhysicianSearchFilters) ([]PhysicianResult, error) {
	resolved := node.Resolved()
	switch strings.ToLower(resolved.SearchMode) {
	case "qido_rs":
		return a.searchPhysicianResultsFromQIDONode(ctx, physician, node, filters)
	case "c_find":
		results, err := a.searchPhysicianResultsFromDIMSENode(ctx, physician, node, filters)
		if err == nil {
			return results, nil
		}
		if isTimeoutLikeError(err) {
			a.log("warn", "physician_cfind_timeout_degraded", map[string]any{
				"physician_id": physician.ID,
				"username":     physician.Username,
				"node_id":      resolved.ID,
				"error":        err.Error(),
			})
			// Keep the physician search responsive when a DIMSE node is slow/unreachable.
			return []PhysicianResult{}, nil
		}
		return nil, err
	default:
		return nil, fmt.Errorf("unsupported physician search mode %s", resolved.SearchMode)
	}
}

func (a *App) searchPhysicianResultsFromQIDONode(ctx context.Context, physician PhysicianSummary, node PACSNodeConfig, filters PhysicianSearchFilters) ([]PhysicianResult, error) {
	resolved := node.Resolved()
	searchStartedAt := time.Now()
	a.log("info", "physician_qido_search_started", map[string]any{
		"physician_id": physician.ID,
		"username":     physician.Username,
		"node_id":      node.ID,
		"patient_id":   filters.PatientID,
		"patient_name": filters.PatientName,
		"date_from":    filters.DateFrom,
		"date_to":      filters.DateTo,
		"modality":     filters.Modality,
	})

	token, err := a.fetchPACSBearerToken(ctx, node)
	if err != nil {
		return nil, fmt.Errorf("fetch pacs token for %s: %w", node.ID, err)
	}

	endpoint, err := url.Parse(strings.TrimRight(resolved.DICOMwebBaseURL, "/") + "/studies")
	if err != nil {
		return nil, fmt.Errorf("build qido url: %w", err)
	}

	query := endpoint.Query()
	query.Set("limit", "50")
	query.Add("includefield", "StudyInstanceUID")
	query.Add("includefield", "StudyDate")
	query.Add("includefield", "StudyDescription")
	query.Add("includefield", "ModalitiesInStudy")
	query.Add("includefield", "PatientName")
	query.Add("includefield", "PatientID")
	query.Add("includefield", "AccessionNumber")
	query.Add("includefield", "NumberOfStudyRelatedInstances")
	if filters.PatientID != "" {
		query.Set("PatientID", filters.PatientID)
	}
	if filters.PatientName != "" {
		query.Set("PatientName", buildPatientNameFuzzyQuery(filters.PatientName))
	}
	if filters.Modality != "" {
		query.Set("ModalitiesInStudy", filters.Modality)
	}
	if filters.DateFrom != "" || filters.DateTo != "" {
		query.Set("StudyDate", buildQIDODateRange(filters.DateFrom, filters.DateTo))
	}
	endpoint.RawQuery = query.Encode()

	a.log("info", "physician_qido_request_started", map[string]any{
		"physician_id": physician.ID,
		"username":     physician.Username,
		"node_id":      node.ID,
		"url":          endpoint.String(),
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build physician qido request: %w", err)
	}
	req.Header.Set("Accept", "application/dicom+json, application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	res, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute physician qido request: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return nil, fmt.Errorf("physician qido bad status %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload []qidoResponseItem
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		if !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("decode physician qido response: %w", err)
		}
		payload = []qidoResponseItem{}
	}

	results := make([]PhysicianResult, 0, len(payload))
	for _, item := range payload {
		studyUID := dicomFirstString(item, "0020000D")
		if studyUID == "" {
			continue
		}
		a.logAccessionNumberProbe("physician_remote_qido", node.ID, studyUID, dicomFirstString(item, "00080050"))

		result := PhysicianResult{
			StudyInstanceUID:    studyUID,
			PatientName:         dicomFirstPersonName(item, "00100010"),
			PatientID:           dicomFirstString(item, "00100020"),
			StudyDate:           normalizeStudyDate(dicomFirstString(item, "00080020")),
			StudyDescription:    dicomFirstString(item, "00081030"),
			NumberOfImages:      dicomFirstInt(item, "00201208"),
			Modalities:          dicomStringList(item, "00080061"),
			Locations:           []string{node.Name},
			SourceNodeID:        node.ID,
			HIS:                 node.HIS,
			CacheStatus:         "not_local",
			RetrieveStatus:      "idle",
			PartialFilter:       false,
			SourceNodeAvailable: a.sourceNodeAvailable(node.ID),
		}

		cacheStatus, retrieveStatus, retrievePhase, retrieveProgress, viewerURL, ohifViewerURL, err := a.getStudyOperationalState(ctx, studyUID, result.CacheStatus, result.RetrieveStatus)
		if err != nil {
			return nil, fmt.Errorf("resolve physician qido state for %s: %w", studyUID, err)
		}
		result.CacheStatus = cacheStatus
		result.RetrieveStatus = retrieveStatus
		result.RetrievePhase = retrievePhase
		result.RetrieveProgress = retrieveProgress
		result.ViewerURL = viewerURL
		result.OHIFViewerURL = ohifViewerURL
		if viewerURL != "" {
			result.DownloadURL = buildPhysicianDownloadURL(studyUID)
		}
		results = append(results, result)
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].StudyDate == results[j].StudyDate {
			return results[i].StudyInstanceUID < results[j].StudyInstanceUID
		}
		return results[i].StudyDate > results[j].StudyDate
	})

	if err := a.persistAndEnrichPhysicianRemoteResults(ctx, physician, node, filters, results); err != nil {
		return nil, err
	}
	if err := a.applyPersistedQIDOCacheToPhysicianResults(ctx, results); err != nil {
		a.log("warn", "physician_remote_cache_overlay_failed", map[string]any{
			"physician_id": physician.ID,
			"node_id":      node.ID,
			"error":        err.Error(),
		})
	}

	a.log("info", "physician_qido_search_completed", map[string]any{
		"physician_id": physician.ID,
		"username":     physician.Username,
		"node_id":      node.ID,
		"result_count": len(results),
		"duration_ms":  time.Since(searchStartedAt).Milliseconds(),
	})

	return results, nil
}

func (a *App) searchPhysicianResultsFromDIMSENode(ctx context.Context, physician PhysicianSummary, node PACSNodeConfig, filters PhysicianSearchFilters) ([]PhysicianResult, error) {
	resolved := node.Resolved()
	searchStartedAt := time.Now()
	a.log("info", "physician_cfind_search_started", map[string]any{
		"physician_id": physician.ID,
		"username":     physician.Username,
		"node_id":      node.ID,
		"patient_id":   filters.PatientID,
		"patient_name": filters.PatientName,
		"date_from":    filters.DateFrom,
		"date_to":      filters.DateTo,
		"modality":     filters.Modality,
	})

	payload, err := a.runOrthancStudyCFind(ctx, node, filters)
	if err != nil {
		return nil, fmt.Errorf("run physician c-find on %s: %w", node.ID, err)
	}

	results := make([]PhysicianResult, 0, len(payload))
	for _, item := range payload {
		studyUID := dicomFirstString(item, "0020000D")
		if studyUID == "" {
			continue
		}

		result := PhysicianResult{
			StudyInstanceUID:    studyUID,
			PatientName:         dicomFirstPersonName(item, "00100010"),
			PatientID:           dicomFirstString(item, "00100020"),
			StudyDate:           normalizeStudyDate(dicomFirstString(item, "00080020")),
			StudyDescription:    dicomFirstString(item, "00081030"),
			NumberOfImages:      dicomFirstInt(item, "00201208"),
			Modalities:          dicomStringList(item, "00080061"),
			Locations:           []string{node.Name},
			SourceNodeID:        resolved.ID,
			HIS:                 node.HIS,
			CacheStatus:         "not_local",
			RetrieveStatus:      "idle",
			PartialFilter:       false,
			SourceNodeAvailable: a.sourceNodeAvailable(node.ID),
		}

		cacheStatus, retrieveStatus, retrievePhase, retrieveProgress, viewerURL, ohifViewerURL, err := a.getStudyOperationalState(ctx, studyUID, result.CacheStatus, result.RetrieveStatus)
		if err != nil {
			return nil, fmt.Errorf("resolve physician c-find state for %s: %w", studyUID, err)
		}
		result.CacheStatus = cacheStatus
		result.RetrieveStatus = retrieveStatus
		result.RetrievePhase = retrievePhase
		result.RetrieveProgress = retrieveProgress
		result.ViewerURL = viewerURL
		result.OHIFViewerURL = ohifViewerURL
		if viewerURL != "" {
			result.DownloadURL = buildPhysicianDownloadURL(studyUID)
		}
		results = append(results, result)
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].StudyDate == results[j].StudyDate {
			return results[i].StudyInstanceUID < results[j].StudyInstanceUID
		}
		return results[i].StudyDate > results[j].StudyDate
	})

	if err := a.persistAndEnrichPhysicianRemoteResults(ctx, physician, node, filters, results); err != nil {
		return nil, err
	}
	if err := a.applyPersistedQIDOCacheToPhysicianResults(ctx, results); err != nil {
		a.log("warn", "physician_remote_cache_overlay_failed", map[string]any{
			"physician_id": physician.ID,
			"node_id":      node.ID,
			"error":        err.Error(),
		})
	}

	a.log("info", "physician_cfind_search_completed", map[string]any{
		"physician_id": physician.ID,
		"username":     physician.Username,
		"node_id":      node.ID,
		"result_count": len(results),
		"duration_ms":  time.Since(searchStartedAt).Milliseconds(),
	})

	return results, nil
}

func (a *App) persistPhysicianRecentQuery(ctx context.Context, physicianID string, filters PhysicianSearchFilters, results []PhysicianResult) error {
	payload := map[string]any{
		"patient_id":   filters.PatientID,
		"patient_name": filters.PatientName,
		"date_from":    filters.DateFrom,
		"date_to":      filters.DateTo,
		"source":       normalizePhysicianSearchSource(filters.Source),
		"modalities":   []string{},
		"results":      results,
	}
	if filters.Modality != "" {
		payload["modalities"] = []string{filters.Modality}
	}

	queryJSON, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	_, err = a.db.ExecContext(ctx, `
		INSERT INTO physician_recent_queries (
			physician_id, query_json, result_count, searched_at, expires_at
		) VALUES (
			$1::uuid, $2::jsonb, $3, now(), now() + interval '7 days'
		)
	`, physicianID, string(queryJSON), len(results))
	return err
}

func (a *App) searchPhysicianResultsFromLocalCache(ctx context.Context, physician PhysicianSummary, filters PhysicianSearchFilters, useInitialCachePeriod bool) ([]PhysicianResult, error) {
	endpoint, err := url.Parse(strings.TrimRight(a.cfg.OrthancURL, "/") + "/dicom-web/studies")
	if err != nil {
		return nil, fmt.Errorf("build orthanc physician cache url: %w", err)
	}

	dateFrom := strings.TrimSpace(filters.DateFrom)
	dateTo := strings.TrimSpace(filters.DateTo)
	if useInitialCachePeriod && !hasPhysicianQueryFilters(filters) {
		period := "current_week"
		if a.externalConfig != nil && strings.TrimSpace(a.externalConfig.Professional.InitialCachePeriod) != "" {
			period = a.externalConfig.Professional.InitialCachePeriod
		}
		dateFrom, dateTo = configuredDateRange(period, time.Now())
	}

	query := endpoint.Query()
	query.Set("limit", "200")
	query.Add("includefield", "StudyInstanceUID")
	query.Add("includefield", "StudyDate")
	query.Add("includefield", "StudyDescription")
	query.Add("includefield", "ModalitiesInStudy")
	query.Add("includefield", "PatientName")
	query.Add("includefield", "PatientID")
	query.Add("includefield", "AccessionNumber")
	query.Add("includefield", "NumberOfStudyRelatedInstances")
	if strings.TrimSpace(filters.PatientID) != "" {
		query.Set("PatientID", strings.TrimSpace(filters.PatientID))
	}
	if strings.TrimSpace(filters.PatientName) != "" {
		query.Set("PatientName", buildPatientNameFuzzyQuery(filters.PatientName))
	}
	if strings.TrimSpace(filters.Modality) != "" {
		query.Set("ModalitiesInStudy", strings.TrimSpace(filters.Modality))
	}
	if dateFrom != "" || dateTo != "" {
		query.Set("StudyDate", buildQIDODateRange(dateFrom, dateTo))
	}
	endpoint.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build orthanc physician cache request: %w", err)
	}
	req.Header.Set("Accept", "application/dicom+json, application/json")
	a.applyOrthancInternalRequestAuth(req)

	res, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute orthanc physician cache request: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return nil, fmt.Errorf("orthanc physician cache bad status %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload []qidoResponseItem
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		if !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("decode orthanc physician cache response: %w", err)
		}
		payload = []qidoResponseItem{}
	}

	results := make([]PhysicianResult, 0, len(payload))
	for _, item := range payload {
		studyUID := dicomFirstString(item, "0020000D")
		if studyUID == "" {
			continue
		}
		a.logAccessionNumberProbe("physician_local_cache_qido", "orthanc", studyUID, dicomFirstString(item, "00080050"))

		cacheStatus, retrieveStatus, retrievePhase, retrieveProgress, viewerURL, ohifViewerURL, err := a.getStudyOperationalState(ctx, studyUID, "local_complete", "done")
		if err != nil {
			return nil, fmt.Errorf("resolve physician cached study state for %s: %w", studyUID, err)
		}
		locations, err := a.cachedStudyLocations(ctx, studyUID)
		if err != nil {
			return nil, fmt.Errorf("load cached study locations for %s: %w", studyUID, err)
		}
		if len(locations) == 0 {
			locations = []string{"Local"}
		}

		results = append(results, PhysicianResult{
			StudyInstanceUID:    studyUID,
			PatientName:         dicomFirstPersonName(item, "00100010"),
			PatientID:           dicomFirstString(item, "00100020"),
			StudyDate:           normalizeStudyDate(dicomFirstString(item, "00080020")),
			StudyDescription:    dicomFirstString(item, "00081030"),
			NumberOfImages:      dicomFirstInt(item, "00201208"),
			Modalities:          dicomStringList(item, "00080061"),
			Locations:           a.resolveLocationLabels(locations),
			CacheStatus:         cacheStatus,
			RetrieveStatus:      retrieveStatus,
			RetrievePhase:       retrievePhase,
			RetrieveProgress:    retrieveProgress,
			PartialFilter:       false,
			ViewerURL:           viewerURL,
			OHIFViewerURL:       ohifViewerURL,
			SourceNodeAvailable: true,
			DownloadURL: func() string {
				if viewerURL == "" {
					return ""
				}
				return buildPhysicianDownloadURL(studyUID)
			}(),
			HIS: func() bool {
				sourceNodeID := a.resolveConfiguredNodeIDForStudy("", locations)
				return a.andesMetadataAvailableForSourceNode(sourceNodeID)
			}(),
		})
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].StudyDate == results[j].StudyDate {
			return results[i].StudyInstanceUID < results[j].StudyInstanceUID
		}
		return results[i].StudyDate > results[j].StudyDate
	})

	if err := a.applyPersistedQIDOCacheToPhysicianResults(ctx, results); err != nil {
		a.log("warn", "physician_local_cache_overlay_failed", map[string]any{
			"physician_id": physician.ID,
			"error":        err.Error(),
		})
	}

	if err := a.persistAndEnrichPhysicianRemoteResults(ctx, physician, PACSNodeConfig{ID: physicianSearchSourceLocalCache}, filters, results); err != nil {
		return nil, err
	}

	return results, nil
}

func uniqueStudyUIDsFromPhysicianResults(results []PhysicianResult) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(results))
	for _, result := range results {
		uid := strings.TrimSpace(result.StudyInstanceUID)
		if uid == "" {
			continue
		}
		if _, ok := seen[uid]; ok {
			continue
		}
		seen[uid] = struct{}{}
		out = append(out, uid)
	}
	return out
}

func (a *App) applyPersistedQIDOCacheToPhysicianResults(ctx context.Context, results []PhysicianResult) error {
	grouped := make(map[string][]string)
	for _, result := range results {
		sourceNodeID := a.resolveConfiguredNodeIDForStudy(result.SourceNodeID, result.Locations)
		if sourceNodeID == "" {
			continue
		}
		grouped[sourceNodeID] = append(grouped[sourceNodeID], result.StudyInstanceUID)
	}

	cacheByNode := make(map[string]map[string]PersistedQIDOStudy, len(grouped))
	for sourceNodeID, studyUIDs := range grouped {
		cache, err := a.loadPersistedQIDOStudies(ctx, sourceNodeID, studyUIDs)
		if err != nil {
			return err
		}
		cacheByNode[sourceNodeID] = cache
	}

	for i := range results {
		sourceNodeID := a.resolveConfiguredNodeIDForStudy(results[i].SourceNodeID, results[i].Locations)
		persisted, ok := cacheByNode[sourceNodeID][results[i].StudyInstanceUID]
		if !ok {
			continue
		}
		if results[i].AndesPrestacionID == "" {
			results[i].AndesPrestacionID = persisted.AndesPrestacionID
		}
		if results[i].AndesPrestacion == "" {
			results[i].AndesPrestacion = persisted.AndesPrestacion
		}
		if results[i].AndesProfessional == "" {
			results[i].AndesProfessional = persisted.AndesProfessional
		}
	}

	return nil
}

func (a *App) persistAndEnrichPhysicianRemoteResults(ctx context.Context, physician PhysicianSummary, node PACSNodeConfig, filters PhysicianSearchFilters, results []PhysicianResult) error {
	_ = ctx
	a.enqueuePhysicianAndesEnrichJob(physician, node, filters, results)
	return nil
}

func (a *App) persistPhysicianResultsToQIDOCache(ctx context.Context, results []PhysicianResult) error {
	cacheEntries := make([]PersistedQIDOStudy, 0, len(results))
	for _, result := range results {
		sourceNodeID := a.resolveConfiguredNodeIDForStudy(result.SourceNodeID, result.Locations)
		if sourceNodeID == "" {
			continue
		}
		cacheEntries = append(cacheEntries, PersistedQIDOStudy{
			StudyInstanceUID:  result.StudyInstanceUID,
			SourceNodeID:      sourceNodeID,
			StudyDate:         result.StudyDate,
			PatientName:       result.PatientName,
			PatientID:         result.PatientID,
			StudyDescription:  result.StudyDescription,
			NumberOfImages:    result.NumberOfImages,
			Modalities:        result.Modalities,
			Locations:         result.Locations,
			AndesPrestacionID: result.AndesPrestacionID,
			AndesPrestacion:   result.AndesPrestacion,
			AndesProfessional: result.AndesProfessional,
		})
	}

	return a.persistQIDOStudies(ctx, cacheEntries)
}

func (a *App) listPhysicianResults(ctx context.Context, physicianID string, filters PhysicianSearchFilters, useInitialCachePeriod bool) ([]PhysicianResult, error) {
	physician := PhysicianSummary{ID: physicianID}
	if err := a.db.QueryRowContext(ctx, `
		SELECT username, COALESCE(dni, ''), COALESCE(full_name, '')
		FROM physicians
		WHERE id = $1::uuid
	`, physicianID).Scan(&physician.Username, &physician.DNI, &physician.FullName); err != nil {
		return nil, fmt.Errorf("load physician summary: %w", err)
	}

	filters.Source = normalizePhysicianSearchSource(filters.Source)
	if filters.Source == physicianSearchSourceLocalCache {
		return a.searchPhysicianResultsFromLocalCache(ctx, physician, filters, useInitialCachePeriod)
	}

	node, ok := a.configuredPACSNodeByID(filters.Source)
	if !ok {
		return nil, fmt.Errorf("unknown physician search source %q", filters.Source)
	}

	return a.searchPhysicianResultsFromNode(ctx, physician, node, filters)
}

func buildPhysicianDownloadURL(studyInstanceUID string) string {
	return "/api/physician/download?study_instance_uid=" + url.QueryEscape(strings.TrimSpace(studyInstanceUID))
}
