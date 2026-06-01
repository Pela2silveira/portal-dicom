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
	"os"
	"sort"
	"strings"
	"time"
)

type PatientAlternateIdentifier struct {
	SourceSystem string
	Type         string
	Value        string
	IsPrimary    bool
}

type PatientStudiesResponse struct {
	DocumentNumber string               `json:"document_number"`
	Patient        PatientSummary       `json:"patient"`
	Filters        PatientStudiesFilter `json:"filters"`
	Sync           PatientSyncStatus    `json:"sync"`
	Studies        []PatientStudy       `json:"studies"`
}

type PatientSyncStatus struct {
	RequestID string `json:"request_id,omitempty"`
	Status    string `json:"status"`
	Message   string `json:"message,omitempty"`
}

type PatientSummary struct {
	ID             string `json:"id"`
	DocumentType   string `json:"document_type"`
	DocumentNumber string `json:"document_number"`
	FullName       string `json:"full_name"`
	BirthDate      string `json:"birth_date"`
	Sex            string `json:"sex"`
	GenderIdentity string `json:"gender_identity"`
}

type PatientStudiesFilter struct {
	DateFrom string `json:"date_from,omitempty"`
	DateTo   string `json:"date_to,omitempty"`
	Modality string `json:"modality,omitempty"`
}

type PatientSearchRequest struct {
	DocumentNumber string `json:"document_number"`
	DateFrom       string `json:"date_from,omitempty"`
	DateTo         string `json:"date_to,omitempty"`
	Modality       string `json:"modality,omitempty"`
}

type PatientStudy struct {
	StudyInstanceUID    string   `json:"study_instance_uid"`
	StudyDate           string   `json:"study_date"`
	StudyDescription    string   `json:"study_description"`
	NumberOfImages      int      `json:"number_of_images,omitempty"`
	ModalitiesInStudy   []string `json:"modalities_in_study"`
	Locations           []string `json:"locations,omitempty"`
	AndesPrestacionID   string   `json:"andes_prestacion_id,omitempty"`
	AndesPrestacion     string   `json:"andes_prestacion,omitempty"`
	AndesProfessional   string   `json:"andes_professional,omitempty"`
	AvailabilityStatus  string   `json:"availability_status"`
	RetrieveStatus      string   `json:"retrieve_status"`
	RetrievePhase       string   `json:"retrieve_phase,omitempty"`
	RetrieveProgress    int      `json:"retrieve_progress,omitempty"`
	AuthorizationBasis  string   `json:"authorization_basis"`
	ViewerURL           string   `json:"viewer_url,omitempty"`
	OHIFViewerURL       string   `json:"ohif_viewer_url,omitempty"`
	DownloadURL         string   `json:"download_url,omitempty"`
	HIS                 bool     `json:"his"`
	SourceNodeAvailable bool     `json:"source_node_available"`
	SourceNodeID        string   `json:"-"`
}

type PatientRetrieveRequest struct {
	DocumentNumber   string `json:"document_number"`
	StudyInstanceUID string `json:"study_instance_uid"`
	// Modality as known by the UI search results (study not local yet at
	// retrieve time); used for the usage "retrieves by modality" breakdown.
	Modality string `json:"modality,omitempty"`
}

type PatientRetrieveResponse struct {
	JobID            string `json:"job_id"`
	StudyInstanceUID string `json:"study_instance_uid"`
	Status           string `json:"status"`
	ViewerURL        string `json:"viewer_url,omitempty"`
	OHIFViewerURL    string `json:"ohif_viewer_url,omitempty"`
}

type PatientStudyPreviewItem struct {
	InstanceID   string `json:"instance_id"`
	ImageDataURL string `json:"image_data_url"`
	DownloadName string `json:"download_name"`
}

type PatientStudyPreviewResponse struct {
	Status           string                    `json:"status"`
	StudyInstanceUID string                    `json:"study_instance_uid"`
	Items            []PatientStudyPreviewItem `json:"items"`
	TotalShown       int                       `json:"total_shown"`
	Limit            int                       `json:"limit"`
	TotalAvailable   int                       `json:"total_available"`
	Truncated        bool                      `json:"truncated"`
}

type PatientStudyShareRequest struct {
	ViewerKind       string `json:"viewer,omitempty"`
	Channel          string `json:"channel,omitempty"`
	ExpiresInHours   int    `json:"expires_in_hours,omitempty"`
	MaxUses          int    `json:"max_uses,omitempty"`
	RecipientLabel   string `json:"recipient_label,omitempty"`
	RecipientContact string `json:"recipient_contact,omitempty"`
}

type PatientStudyShareResponse struct {
	Status           string `json:"status"`
	StudyInstanceUID string `json:"study_instance_uid"`
	ViewerKind       string `json:"viewer_kind"`
	ShareURL         string `json:"share_url"`
	QRCodeDataURL    string `json:"qr_code_data_url,omitempty"`
	WhatsAppURL      string `json:"whatsapp_url,omitempty"`
	MailToURL        string `json:"mailto_url,omitempty"`
	ExpiresAt        string `json:"expires_at"`
	MaxUses          int    `json:"max_uses"`
}

type PatientSendCodeRequest struct {
	DocumentNumber string `json:"document_number"`
}

type PatientSendCodeResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

type PatientLoginRequest struct {
	DocumentNumber string `json:"document_number"`
	Code           string `json:"code"`
}

type PatientLoginResponse struct {
	Status       string         `json:"status"`
	Message      string         `json:"message"`
	Patient      PatientSummary `json:"patient,omitempty"`
	SessionToken string         `json:"session_token,omitempty"`
}

type patientSessionSnapshot struct {
	SessionID string
	PatientID string
	ExpiresAt time.Time
	Status    string
}

type RuntimePatientConfigResponse struct {
	AuthMode string `json:"auth_mode"`
}

type PatientConfig struct {
	AuthMode        string   `json:"auth_mode"`
	FakeAuth        bool     `json:"fake_auth,omitempty"`
	MatchDebugNodes []string `json:"match_debug_nodes,omitempty"`
}

const (
	PatientAuthModeMail        = "mail"
	PatientAuthModeFakeAuth    = "fake_auth"
	PatientAuthModeMasterKey   = "master_key"
	orthancInternalTokenHeader = "X-Orthanc-Internal-Token"
)

func (c PatientConfig) ResolvedAuthMode() string {
	mode := strings.ToLower(strings.TrimSpace(c.AuthMode))
	switch mode {
	case PatientAuthModeMail, PatientAuthModeFakeAuth, PatientAuthModeMasterKey:
		return mode
	case "":
		if c.FakeAuth {
			return PatientAuthModeFakeAuth
		}
		return PatientAuthModeMail
	default:
		return mode
	}
}

func (a *App) startPatientSearchWorker() {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := a.reconcileStalePatientSearches(ctx); err != nil {
			a.log("error", "patient_search_reconcile_failed", map[string]any{"error": err.Error()})
		}
	}()

	go func() {
		for requestID := range a.patientSearchQueue {
			a.processPatientSearchRequest(requestID)
		}
	}()
}

func (a *App) reconcileStalePatientSearches(ctx context.Context) error {
	rows, err := a.db.QueryContext(ctx, `
		UPDATE search_requests
		SET status = 'failed',
		    finished_at = now()
		WHERE actor_type = 'patient'
		  AND status IN ('queued', 'running')
		RETURNING id::text
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	var reconciled int
	for rows.Next() {
		var requestID string
		if err := rows.Scan(&requestID); err != nil {
			return err
		}
		reconciled++
		if _, err := a.db.ExecContext(ctx, `
			UPDATE search_node_runs
			SET status = 'failed',
			    finished_at = now(),
			    error = COALESCE(NULLIF(error, ''), 'search interrupted by backend restart')
			WHERE search_request_id = $1::uuid
			  AND status IN ('queued', 'running')
		`, requestID); err != nil {
			return err
		}
	}

	if err := rows.Err(); err != nil {
		return err
	}
	if reconciled > 0 {
		a.log("info", "patient_searches_reconciled_after_restart", map[string]any{
			"count": reconciled,
		})
	}
	return nil
}

func (a *App) enqueuePatientSearch(requestID string) {
	a.patientSearchQueue <- requestID
}

func (a *App) handlePatientSendCode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var reqBody PatientSendCodeRequest
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		writePatientSendCodeResponse(w, http.StatusBadRequest, "invalid_request", "JSON inválido.")
		return
	}

	reqBody.DocumentNumber = strings.TrimSpace(reqBody.DocumentNumber)
	if reqBody.DocumentNumber == "" {
		writePatientSendCodeResponse(w, http.StatusBadRequest, "invalid_request", "document_number es requerido.")
		return
	}
	if err := validateDocumentNumber(reqBody.DocumentNumber); err != nil {
		writePatientSendCodeResponse(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if !a.enforceLoginRateLimit(w, r, patientLoginRateLimitPolicy("patient_send_code"), reqBody.DocumentNumber) {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	patient, identity, err := a.ensurePatientRecordWithIdentity(ctx, reqBody.DocumentNumber)
	if err != nil {
		if errors.Is(err, ErrPatientIdentityNotFound) {
			a.log("info", "patient_send_code_patient_not_found", map[string]any{
				"document_number": reqBody.DocumentNumber,
				"provider":        a.identitySource.ProviderName(),
			})
			writePatientSendCodeResponse(w, http.StatusNotFound, "patient_not_found", "El paciente no cuenta con registros.")
			return
		}

		a.log("error", "patient_send_code_failed", map[string]any{
			"document_number": reqBody.DocumentNumber,
			"provider":        a.identitySource.ProviderName(),
			"error":           err.Error(),
		})
		writePatientSendCodeResponse(w, http.StatusBadGateway, "provider_unavailable", "No se pudo validar el contacto del paciente.")
		return
	}

	patientAuthMode := a.resolvedPatientAuthMode()

	if patientAuthMode == PatientAuthModeFakeAuth {
		maskedEmail := maskPatientEmail(identity.Email)
		a.log("info", "patient_send_code_ready_fake_auth", map[string]any{
			"document_number": reqBody.DocumentNumber,
			"patient_id":      patient.ID,
			"provider":        a.identitySource.ProviderName(),
		})
		writePatientSendCodeResponse(w, http.StatusOK, "ready_to_send", patientSendCodeReadyMessage(maskedEmail, true))
		return
	}

	if patientAuthMode == PatientAuthModeMasterKey {
		if strings.TrimSpace(patientMasterKey()) == "" {
			a.log("error", "patient_send_code_master_key_not_configured", map[string]any{
				"document_number": reqBody.DocumentNumber,
				"patient_id":      patient.ID,
				"provider":        a.identitySource.ProviderName(),
			})
			writePatientSendCodeResponse(w, http.StatusServiceUnavailable, "provider_unavailable", "La autenticación por llave maestra no está configurada.")
			return
		}
		a.log("info", "patient_send_code_ready_master_key", map[string]any{
			"document_number": reqBody.DocumentNumber,
			"patient_id":      patient.ID,
			"provider":        a.identitySource.ProviderName(),
		})
		writePatientSendCodeResponse(w, http.StatusOK, "ready_to_send", patientSendCodeReadyMessage("", true))
		return
	}

	if strings.TrimSpace(identity.Email) == "" {
		a.log("info", "patient_send_code_missing_email", map[string]any{
			"document_number": reqBody.DocumentNumber,
			"patient_id":      patient.ID,
			"provider":        a.identitySource.ProviderName(),
		})
		writePatientSendCodeResponse(w, http.StatusConflict, "missing_active_email", "El paciente no tiene mail asociado. Concurra a su centro de salud más cercano para la actualización de sus datos de contacto.")
		return
	}

	code, err := generateNumericCode(6)
	if err != nil {
		a.log("error", "patient_send_code_generation_failed", map[string]any{
			"document_number": reqBody.DocumentNumber,
			"patient_id":      patient.ID,
			"provider":        a.identitySource.ProviderName(),
			"error":           err.Error(),
		})
		writePatientSendCodeResponse(w, http.StatusInternalServerError, "internal_error", "No se pudo generar el código de acceso.")
		return
	}
	if err := a.storePatientMailCode(ctx, patient.ID, code, 10*time.Minute); err != nil {
		a.log("error", "patient_send_code_store_failed", map[string]any{
			"document_number": reqBody.DocumentNumber,
			"patient_id":      patient.ID,
			"provider":        a.identitySource.ProviderName(),
			"error":           err.Error(),
		})
		writePatientSendCodeResponse(w, http.StatusInternalServerError, "internal_error", "No se pudo registrar el código de acceso.")
		return
	}
	if err := sendPatientAccessCodeMail(ctx, identity.Email, code); err != nil {
		a.log("error", "patient_send_code_mail_delivery_failed", map[string]any{
			"document_number": reqBody.DocumentNumber,
			"patient_id":      patient.ID,
			"provider":        a.identitySource.ProviderName(),
			"error":           err.Error(),
		})
		writePatientSendCodeResponse(w, http.StatusBadGateway, "delivery_failed", "No se pudo enviar el código por mail.")
		return
	}

	a.log("info", "patient_send_code_ready", map[string]any{
		"document_number": reqBody.DocumentNumber,
		"patient_id":      patient.ID,
		"provider":        a.identitySource.ProviderName(),
	})
	writePatientSendCodeResponse(w, http.StatusOK, "ready_to_send", patientSendCodeReadyMessage(maskPatientEmail(identity.Email), false))
}

func (a *App) handlePatientLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var reqBody PatientLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		writePatientLoginResponse(w, http.StatusBadRequest, "invalid_request", "JSON inválido.", PatientSummary{})
		return
	}

	reqBody.DocumentNumber = strings.TrimSpace(reqBody.DocumentNumber)
	reqBody.Code = strings.TrimSpace(reqBody.Code)
	if reqBody.DocumentNumber == "" {
		writePatientLoginResponse(w, http.StatusBadRequest, "invalid_request", "document_number es requerido.", PatientSummary{})
		return
	}
	if reqBody.Code == "" {
		writePatientLoginResponse(w, http.StatusBadRequest, "invalid_request", "code es requerido.", PatientSummary{})
		return
	}
	if err := validateDocumentNumber(reqBody.DocumentNumber); err != nil {
		writePatientLoginResponse(w, http.StatusBadRequest, "invalid_request", err.Error(), PatientSummary{})
		return
	}
	setActionDim(r.Context(), "identifier", reqBody.DocumentNumber)
	if !a.enforceLoginRateLimit(w, r, patientLoginRateLimitPolicy("patient_login"), reqBody.DocumentNumber) {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	patient, _, err := a.ensurePatientRecordWithIdentity(ctx, reqBody.DocumentNumber)
	if err != nil {
		if errors.Is(err, ErrPatientIdentityNotFound) {
			writePatientLoginResponse(w, http.StatusNotFound, "patient_not_found", "El paciente no cuenta con registros.", PatientSummary{})
			return
		}
		writePatientLoginResponse(w, http.StatusBadGateway, "provider_unavailable", "No se pudo validar el paciente.", PatientSummary{})
		return
	}

	patientAuthMode := a.resolvedPatientAuthMode()

	if patientAuthMode == PatientAuthModeMasterKey {
		expected := strings.TrimSpace(patientMasterKey())
		if expected == "" {
			writePatientLoginResponse(w, http.StatusServiceUnavailable, "provider_unavailable", "La autenticación por llave maestra no está configurada.", PatientSummary{})
			return
		}
		if subtle.ConstantTimeCompare([]byte(reqBody.Code), []byte(expected)) != 1 {
			writePatientLoginResponse(w, http.StatusUnauthorized, "invalid_code", "El código ingresado no es válido.", PatientSummary{})
			return
		}
	}
	if patientAuthMode == PatientAuthModeMail {
		valid, err := a.consumePatientMailCode(ctx, patient.ID, reqBody.Code)
		if err != nil {
			writePatientLoginResponse(w, http.StatusInternalServerError, "internal_error", "No se pudo validar el código de acceso.", PatientSummary{})
			return
		}
		if !valid {
			writePatientLoginResponse(w, http.StatusUnauthorized, "invalid_code", "El código ingresado no es válido.", PatientSummary{})
			return
		}
	}

	_, rawSessionToken, expiresAt, err := a.createPatientSession(ctx, patient.ID, r)
	if err != nil {
		writePatientLoginResponse(w, http.StatusInternalServerError, "internal_error", "No se pudo crear la sesión del paciente.", PatientSummary{})
		return
	}
	setPortalSessionCookie(w, r, patientSessionCookieName, rawSessionToken, expiresAt)

	setActionDim(r.Context(), "patient_id", patient.ID)
	setActionDim(r.Context(), "auth_mode", patientAuthMode)
	writePatientLoginResponse(w, http.StatusOK, "ok", "Acceso validado.", patient)
}

func patientMasterKey() string {
	return strings.TrimSpace(os.Getenv("PATIENT_MASTER_KEY"))
}

func (a *App) resolvedPatientAuthMode() string {
	if a != nil && a.externalConfig != nil {
		return a.externalConfig.Patient.ResolvedAuthMode()
	}
	return PatientAuthModeFakeAuth
}

func writePatientSendCodeResponse(w http.ResponseWriter, statusCode int, status, message string) {
	writeJSON(w, statusCode, PatientSendCodeResponse{
		Status:  strings.TrimSpace(status),
		Message: strings.TrimSpace(message),
	})
}

func writePatientLoginResponse(w http.ResponseWriter, statusCode int, status, message string, patient PatientSummary) {
	payload := PatientLoginResponse{
		Status:  strings.TrimSpace(status),
		Message: strings.TrimSpace(message),
	}
	if strings.TrimSpace(patient.ID) != "" {
		payload.Patient = patient
	}
	writeJSON(w, statusCode, payload)
}

func (a *App) handlePatientLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	rawToken := sessionCookieToken(r, patientSessionCookieName)
	if rawToken != "" {
		if err := a.invalidatePatientSessionByToken(ctx, rawToken); err != nil && !errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "failed to logout patient session", http.StatusInternalServerError)
			return
		}
	}

	clearPortalSessionCookie(w, r, patientSessionCookieName)
	clearViewerGrantCookie(w, r)
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "logged_out",
	})
}

func (a *App) handlePatientSearch(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		a.handlePatientSearchStart(w, r)
	case http.MethodGet:
		a.handlePatientSearchStatus(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *App) handlePatientSearchStart(w http.ResponseWriter, r *http.Request) {
	var reqBody PatientSearchRequest
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}

	reqBody.DocumentNumber = strings.TrimSpace(reqBody.DocumentNumber)
	if reqBody.DocumentNumber == "" {
		http.Error(w, "document_number is required", http.StatusBadRequest)
		return
	}
	if err := validateDocumentNumber(reqBody.DocumentNumber); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	filters := PatientStudiesFilter{
		DateFrom: strings.TrimSpace(reqBody.DateFrom),
		DateTo:   strings.TrimSpace(reqBody.DateTo),
		Modality: strings.ToUpper(strings.TrimSpace(reqBody.Modality)),
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()

	_, patient, err := a.requirePatientSessionSummary(ctx, r)
	if err != nil {
		http.Error(w, "invalid session", http.StatusUnauthorized)
		return
	}
	if reqBody.DocumentNumber != "" && subtle.ConstantTimeCompare([]byte(reqBody.DocumentNumber), []byte(patient.DocumentNumber)) != 1 {
		http.Error(w, "patient session does not match requested document", http.StatusForbidden)
		return
	}
	state, err := a.ensurePatientSearchRequest(ctx, patient, patient.DocumentNumber, filters)
	if err != nil {
		a.log("error", "patient_search_enqueue_failed", map[string]any{
			"document_number": patient.DocumentNumber,
			"patient_id":      patient.ID,
			"error":           err.Error(),
		})
		http.Error(w, "failed to enqueue patient search", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusAccepted, state)
}

func (a *App) handlePatientSearchStatus(w http.ResponseWriter, r *http.Request) {
	requestID := strings.TrimSpace(r.URL.Query().Get("request_id"))
	if requestID == "" {
		http.Error(w, "request_id is required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	session, _, err := a.requirePatientSessionSummary(ctx, r)
	if err != nil {
		http.Error(w, "invalid session", http.StatusUnauthorized)
		return
	}

	state, err := a.getPatientSearchStateByRequestID(ctx, session.PatientID, requestID)
	if err != nil {
		a.log("error", "patient_search_status_failed", map[string]any{
			"request_id": requestID,
			"error":      err.Error(),
		})
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "patient search request not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to load patient search status", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, state)
}

func (a *App) handlePatientStudies(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	documentNumber := strings.TrimSpace(r.URL.Query().Get("document"))

	filters := PatientStudiesFilter{
		DateFrom: strings.TrimSpace(r.URL.Query().Get("date_from")),
		DateTo:   strings.TrimSpace(r.URL.Query().Get("date_to")),
		Modality: strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("modality"))),
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()

	_, patient, err := a.requirePatientSessionSummary(ctx, r)
	if err != nil {
		http.Error(w, "invalid session", http.StatusUnauthorized)
		return
	}
	if documentNumber != "" {
		if err := validateDocumentNumber(documentNumber); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if subtle.ConstantTimeCompare([]byte(documentNumber), []byte(patient.DocumentNumber)) != 1 {
			http.Error(w, "patient session does not match requested document", http.StatusForbidden)
			return
		}
	}

	syncState, err := a.getPatientSearchState(ctx, patient.ID, filters)
	if err != nil {
		a.log("error", "patient_search_state_failed", map[string]any{
			"document_number": patient.DocumentNumber,
			"patient_id":      patient.ID,
			"error":           err.Error(),
		})
		http.Error(w, "failed to prepare patient search", http.StatusInternalServerError)
		return
	}

	studies, err := a.listPatientStudies(ctx, patient.ID, patient.DocumentNumber, filters)
	if err != nil {
		a.log("error", "patient_studies_query_failed", map[string]any{
			"document_number": patient.DocumentNumber,
			"patient_id":      patient.ID,
			"error":           err.Error(),
		})
		http.Error(w, "failed to query patient studies", http.StatusInternalServerError)
		return
	}

	resp := PatientStudiesResponse{
		DocumentNumber: patient.DocumentNumber,
		Patient:        patient,
		Filters:        filters,
		Sync:           syncState,
		Studies:        studies,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (a *App) handlePatientDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	documentNumber := strings.TrimSpace(r.URL.Query().Get("document"))
	studyInstanceUID := strings.TrimSpace(r.URL.Query().Get("study_instance_uid"))
	if studyInstanceUID == "" {
		http.Error(w, "missing required query params", http.StatusBadRequest)
		return
	}
	setActionDim(r.Context(), "study_uid", studyInstanceUID)

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	_, patient, err := a.requirePatientSessionSummary(ctx, r)
	if err != nil {
		http.Error(w, "invalid session", http.StatusUnauthorized)
		return
	}
	if documentNumber != "" {
		if err := validateDocumentNumber(documentNumber); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if subtle.ConstantTimeCompare([]byte(documentNumber), []byte(patient.DocumentNumber)) != 1 {
			http.Error(w, "patient session does not match requested document", http.StatusForbidden)
			return
		}
	}
	authorized, err := a.patientStudyAvailableLocal(ctx, patient.ID, studyInstanceUID)
	if err != nil {
		http.Error(w, "failed to validate patient study", http.StatusInternalServerError)
		return
	}
	if !authorized {
		http.Error(w, "study not available for patient download", http.StatusNotFound)
		return
	}

	if mods, mErr := a.patientStudyModalities(ctx, patient.ID, studyInstanceUID); mErr == nil {
		setActionDim(r.Context(), "modality", usageModalityDim(mods))
	}

	if err := a.streamStudyArchiveByUID(ctx, w, studyInstanceUID); err != nil {
		a.log("error", "patient_download_failed", map[string]any{
			"patient_id":         patient.ID,
			"study_instance_uid": studyInstanceUID,
			"error":              err.Error(),
		})
		http.Error(w, "failed to download study archive", http.StatusBadGateway)
	}
}

func (a *App) handlePatientStudyRoute(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasSuffix(r.URL.Path, "/access"):
		a.action(ActionViewerAccessGrant, a.handlePatientStudyAccess)(w, r)
	case strings.HasSuffix(r.URL.Path, "/preview"):
		a.handlePatientStudyPreview(w, r)
	case strings.HasSuffix(r.URL.Path, "/share"):
		a.action(ActionShareLinkCreate, a.handlePatientStudyShare)(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (a *App) handlePatientStudyAccess(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	studyUID := studyUIDFromAccessPath(r.URL.Path, "/api/patient/studies/")
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
	rawSessionToken := sessionCookieToken(r, patientSessionCookieName)
	if rawSessionToken == "" {
		http.Error(w, "missing session token", http.StatusUnauthorized)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	session, err := a.patientSessionByToken(ctx, rawSessionToken)
	if err != nil {
		http.Error(w, "invalid session", http.StatusUnauthorized)
		return
	}

	authorized, err := a.patientStudyAvailableLocal(ctx, session.PatientID, studyUID)
	if err != nil {
		http.Error(w, "failed to validate patient study access", http.StatusInternalServerError)
		return
	}
	if !authorized {
		http.Error(w, "study not authorized for patient viewer", http.StatusNotFound)
		return
	}

	accessURL, expiresAt, err := a.createViewerAccessGrant(ctx, "patient", session.SessionID, "", studyUID, viewerKind, r, session.ExpiresAt)
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

func (a *App) handlePatientStudyPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	studyUID := studyUIDFromActionPath(r.URL.Path, "/api/patient/studies/", "/preview")
	if studyUID == "" {
		http.NotFound(w, r)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	_, patient, err := a.requirePatientSessionSummary(ctx, r)
	if err != nil {
		http.Error(w, "invalid session", http.StatusUnauthorized)
		return
	}

	authorized, err := a.patientStudyAvailableLocal(ctx, patient.ID, studyUID)
	if err != nil {
		http.Error(w, "failed to validate patient study access", http.StatusInternalServerError)
		return
	}
	if !authorized {
		http.Error(w, "study not available for patient preview", http.StatusNotFound)
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
		Limit:            previewLimit,
		TotalShown:       len(items),
		TotalAvailable:   totalAvailable,
		Truncated:        totalAvailable > len(items),
	})
}

func (a *App) handlePatientStudyShare(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	studyUID := studyUIDFromActionPath(r.URL.Path, "/api/patient/studies/", "/share")
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

	// The action() decorator already resolved + authorized the patient actor;
	// reuse it to avoid a second session lookup. Fall back to resolving the
	// session directly if the handler is ever called outside the decorator.
	patientID := ""
	if actor, ok := actorFromContext(ctx); ok && actor.Kind == SubjectPatient {
		patientID = actor.ID
	} else {
		_, patient, err := a.requirePatientSessionSummary(ctx, r)
		if err != nil {
			http.Error(w, "invalid session", http.StatusUnauthorized)
			return
		}
		patientID = patient.ID
	}

	setActionDim(ctx, "study_uid", studyUID)
	setActionDim(ctx, "channel", channel)
	setActionDim(ctx, "viewer_kind", viewerKind)

	authorized, err := a.patientStudyAvailableLocal(ctx, patientID, studyUID)
	if err != nil {
		http.Error(w, "failed to validate patient study access", http.StatusInternalServerError)
		return
	}
	if !authorized {
		http.Error(w, "study not available for patient share", http.StatusNotFound)
		return
	}

	shareURL, rawToken, expiresAt, maxUses, err := a.createStudyShareLink(
		ctx,
		patientID,
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

	shareMessage := fmt.Sprintf("Te comparto mi estudio de diagnóstico por imágenes. Está disponible hasta el %s.", expiresAt.In(time.UTC).Format("2006-01-02 15:04 UTC"))
	qrCodeDataURL, err := buildStudyShareQRCodeDataURL(shareURL)
	if err != nil {
		http.Error(w, "failed to build share qr", http.StatusInternalServerError)
		return
	}
	whatsAppURL := "https://wa.me/?text=" + url.QueryEscape(shareMessage+" "+shareURL)
	mailSubject := "Estudio por imágenes compartido | Salud Pública Neuquén"
	mailBody := shareMessage + "\n\n" + shareURL
	mailToURL := "mailto:?subject=" + url.QueryEscape(mailSubject) + "&body=" + url.QueryEscape(mailBody)

	a.log("info", "patient_study_share_created", map[string]any{
		"patient_id":         patientID,
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

func patientSearchQueryJSON(documentNumber string, filters PatientStudiesFilter) (string, error) {
	payload, err := json.Marshal(map[string]any{
		"document_number": documentNumber,
		"date_from":       filters.DateFrom,
		"date_to":         filters.DateTo,
		"modality":        filters.Modality,
	})
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

func (a *App) ensurePatientSearchRequest(ctx context.Context, patient PatientSummary, documentNumber string, filters PatientStudiesFilter) (PatientSyncStatus, error) {
	queryJSON, err := patientSearchQueryJSON(documentNumber, filters)
	if err != nil {
		return PatientSyncStatus{}, err
	}

	var existing PatientSyncStatus
	err = a.db.QueryRowContext(ctx, `
		SELECT id::text, status
		FROM search_requests
		WHERE actor_type = 'patient'
		  AND patient_id = $1::uuid
		  AND query_json = $2::jsonb
		  AND status IN ('queued', 'running')
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, patient.ID, queryJSON).Scan(&existing.RequestID, &existing.Status)
	if err == nil {
		existing.Message = patientSyncMessage(existing.Status)
		return existing, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return PatientSyncStatus{}, err
	}

	var requestID string
	err = a.db.QueryRowContext(ctx, `
		INSERT INTO search_requests (
			actor_type, patient_id, query_json, status
		) VALUES (
			'patient', $1::uuid, $2::jsonb, 'queued'
		)
		RETURNING id::text
	`, patient.ID, queryJSON).Scan(&requestID)
	if err != nil {
		return PatientSyncStatus{}, err
	}

	node := a.externalConfig.PACSNodes[0]
	if _, err := a.db.ExecContext(ctx, `
		INSERT INTO search_node_runs (
			search_request_id, node_id, status
		) VALUES (
			$1::uuid, (SELECT id FROM pacs_nodes WHERE code = $2), 'queued'
		)
	`, requestID, node.ID); err != nil {
		return PatientSyncStatus{}, err
	}

	a.enqueuePatientSearch(requestID)

	return PatientSyncStatus{
		RequestID: requestID,
		Status:    "queued",
		Message:   patientSyncMessage("queued"),
	}, nil
}

func (a *App) getPatientSearchState(ctx context.Context, patientID string, filters PatientStudiesFilter) (PatientSyncStatus, error) {
	queryJSON, err := json.Marshal(map[string]any{
		"date_from": filters.DateFrom,
		"date_to":   filters.DateTo,
		"modality":  filters.Modality,
	})
	if err != nil {
		return PatientSyncStatus{}, err
	}

	var state PatientSyncStatus
	err = a.db.QueryRowContext(ctx, `
		SELECT id::text, status
		FROM search_requests
		WHERE actor_type = 'patient'
		  AND patient_id = $1::uuid
		  AND query_json->>'date_from' = ($2::jsonb)->>'date_from'
		  AND query_json->>'date_to' = ($2::jsonb)->>'date_to'
		  AND query_json->>'modality' = ($2::jsonb)->>'modality'
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, patientID, string(queryJSON)).Scan(&state.RequestID, &state.Status)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return PatientSyncStatus{Status: "idle"}, nil
		}
		return PatientSyncStatus{}, err
	}
	state.Message = patientSyncMessage(state.Status)
	return state, nil
}

func (a *App) getPatientSearchStateByRequestID(ctx context.Context, patientID, requestID string) (PatientSyncStatus, error) {
	var state PatientSyncStatus
	err := a.db.QueryRowContext(ctx, `
		SELECT id::text, status
		FROM search_requests
		WHERE id = $1::uuid
		  AND actor_type = 'patient'
		  AND patient_id = $2::uuid
	`, requestID, patientID).Scan(&state.RequestID, &state.Status)
	if err != nil {
		return PatientSyncStatus{}, err
	}
	state.Message = patientSyncMessage(state.Status)
	return state, nil
}

func patientSyncMessage(status string) string {
	switch status {
	case "queued":
		return "Buscando..."
	case "running":
		return "Buscando..."
	case "failed":
		return "No se pudo completar la búsqueda remota."
	default:
		return ""
	}
}

func (a *App) handlePatientRetrieve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var reqBody PatientRetrieveRequest
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}

	reqBody.DocumentNumber = strings.TrimSpace(reqBody.DocumentNumber)
	reqBody.StudyInstanceUID = strings.TrimSpace(reqBody.StudyInstanceUID)
	if reqBody.StudyInstanceUID == "" {
		http.Error(w, "study_instance_uid is required", http.StatusBadRequest)
		return
	}
	setActionDim(r.Context(), "study_uid", reqBody.StudyInstanceUID)
	setActionDim(r.Context(), "modality", usageModalityDim(strings.Split(reqBody.Modality, "/")))

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	_, patient, err := a.requirePatientSessionSummary(ctx, r)
	if err != nil {
		http.Error(w, "invalid session", http.StatusUnauthorized)
		return
	}
	if reqBody.DocumentNumber != "" {
		if err := validateDocumentNumber(reqBody.DocumentNumber); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if subtle.ConstantTimeCompare([]byte(reqBody.DocumentNumber), []byte(patient.DocumentNumber)) != 1 {
			http.Error(w, "patient session does not match requested document", http.StatusForbidden)
			return
		}
	}

	resp, err := a.queuePatientRetrieve(ctx, patient, reqBody.StudyInstanceUID)
	if err != nil {
		statusCode := http.StatusBadGateway
		if errors.Is(err, ErrSourceNodeUnavailable) {
			statusCode = http.StatusServiceUnavailable
		}
		a.log("error", "patient_retrieve_failed", map[string]any{
			"document_number":    patient.DocumentNumber,
			"patient_id":         patient.ID,
			"study_instance_uid": reqBody.StudyInstanceUID,
			"error":              err.Error(),
		})
		http.Error(w, err.Error(), statusCode)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

const (
	patientSessionCookieName   = "portal_patient_session"
	physicianSessionCookieName = "portal_physician_session"
	viewerGrantCookieName      = "portal_viewer_grants"
	shareLinkCookieName        = "portal_share_links"
	viewerGrantCookieSeparator = "."
	shareLinkCookieSeparator   = "."
	shareTokenKeyName          = "token"
)

func patientLoginRateLimitPolicy(endpoint string) LoginRateLimitPolicy {
	switch endpoint {
	case "patient_send_code":
		return LoginRateLimitPolicy{
			Endpoint: endpoint,
			Rules: []LoginRateLimitRule{
				{Scope: "ip", Limit: 10, Window: time.Minute},
				{Scope: "identifier", Limit: 3, Window: 10 * time.Minute},
			},
		}
	case "patient_login":
		return LoginRateLimitPolicy{
			Endpoint: endpoint,
			Rules: []LoginRateLimitRule{
				{Scope: "ip", Limit: 20, Window: time.Minute},
				{Scope: "identifier", Limit: 5, Window: 10 * time.Minute},
			},
		}
	default:
		return LoginRateLimitPolicy{Endpoint: endpoint}
	}
}

func patientSendCodeReadyMessage(maskedEmail string, demo bool) string {
	if maskedEmail == "" {
		if demo {
			return "Modo demo activo. Se omite la validación real del correo."
		}
		return "Se enviará un código por mail al contacto registrado."
	}

	if demo {
		return "Modo demo activo. Se ha enviado el código a " + maskedEmail + "."
	}
	return "Se ha enviado el código a " + maskedEmail + "."
}

func sendPatientAccessCodeMail(ctx context.Context, recipient, code string) error {
	recipient = strings.TrimSpace(recipient)
	code = strings.TrimSpace(code)
	if recipient == "" {
		return errors.New("missing recipient email")
	}
	if code == "" {
		return errors.New("missing code")
	}
	cfg, err := smtpConfigFromEnv()
	if err != nil {
		return err
	}

	subject := "Codigo de acceso - Portal de Imagenes"
	body := "Su codigo de acceso es: " + code + "\n\nSi usted no solicito este codigo, ignore este mensaje."
	return sendSMTPPlainMail(ctx, cfg, recipient, subject, body)
}

func maskPatientEmail(email string) string {
	trimmed := strings.TrimSpace(email)
	parts := strings.Split(trimmed, "@")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return ""
	}

	localPart := []rune(parts[0])
	if len(localPart) <= 3 {
		return trimmed
	}

	masked := make([]rune, len(localPart))
	copy(masked[:3], localPart[:3])
	for i := 3; i < len(localPart); i++ {
		masked[i] = '*'
	}

	return string(masked) + "@" + parts[1]
}

func buildPatientNameFuzzyQuery(value string) string {
	tokens := tokenizeFuzzySearch(value)
	if len(tokens) == 0 {
		return ""
	}
	return "*" + strings.Join(tokens, "*") + "*"
}

func buildPatientSurnameCFindQuery(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	surnamePart := trimmed
	if idx := strings.Index(trimmed, ","); idx >= 0 {
		surnamePart = strings.TrimSpace(trimmed[:idx])
	}
	tokens := tokenizeFuzzySearch(surnamePart)
	if len(tokens) == 0 {
		return ""
	}
	return tokens[0] + "*"
}

type remotePatientMatchCandidate struct {
	NodeID           string
	StudyInstanceUID string
	PatientID        string
	PatientName      string
	BirthDate        string
	Sex              string
}

func (a *App) shouldLogPatientMatchDebug(nodeID string) bool {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return false
	}
	for _, configured := range a.externalConfig.Patient.MatchDebugNodes {
		if strings.EqualFold(strings.TrimSpace(configured), nodeID) {
			return true
		}
	}
	return false
}

func patientIdentifierSet(identifiers []PatientAlternateIdentifier) map[string]PatientAlternateIdentifier {
	index := make(map[string]PatientAlternateIdentifier, len(identifiers))
	for _, identifier := range identifiers {
		value := strings.TrimSpace(identifier.Value)
		if value == "" {
			continue
		}
		index[value] = identifier
	}
	return index
}

func patientNameTokenSet(value string) map[string]struct{} {
	tokens := tokenizeFuzzySearch(value)
	if len(tokens) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(tokens))
	for _, token := range tokens {
		if token == "" {
			continue
		}
		set[token] = struct{}{}
	}
	return set
}

func sharedPatientNameTokenCount(left, right map[string]struct{}) int {
	if len(left) == 0 || len(right) == 0 {
		return 0
	}
	if len(left) > len(right) {
		left, right = right, left
	}
	shared := 0
	for token := range left {
		if _, ok := right[token]; ok {
			shared++
		}
	}
	return shared
}

func hasHighPatientNameMatch(left, right string) bool {
	leftNormalized := normalizeFuzzySearchText(left)
	rightNormalized := normalizeFuzzySearchText(right)
	if leftNormalized == "" || rightNormalized == "" {
		return false
	}
	if leftNormalized == rightNormalized {
		return true
	}

	leftSet := patientNameTokenSet(leftNormalized)
	rightSet := patientNameTokenSet(rightNormalized)
	shared := sharedPatientNameTokenCount(leftSet, rightSet)
	if shared == 0 {
		return false
	}

	leftCount := len(leftSet)
	rightCount := len(rightSet)
	minCount := leftCount
	if rightCount < minCount {
		minCount = rightCount
	}
	if minCount == 0 {
		return false
	}
	if shared == minCount && minCount >= 2 {
		return true
	}
	if minCount >= 3 && shared >= minCount-1 {
		return true
	}
	return false
}

func patientDemographicMatch(patient PatientSummary, candidate remotePatientMatchCandidate) bool {
	hisBirthDate := strings.TrimSpace(patient.BirthDate)
	remoteBirthDate := normalizeRemoteBirthDate(candidate.BirthDate)
	if hisBirthDate == "" || remoteBirthDate == "" || hisBirthDate != remoteBirthDate {
		return false
	}
	hisSex := normalizeRemoteSex(patient.Sex)
	remoteSex := normalizeRemoteSex(candidate.Sex)
	if hisSex == "" || remoteSex == "" || hisSex != remoteSex {
		return false
	}
	return hasHighPatientNameMatch(patient.FullName, candidate.PatientName)
}

func matchesPatientNameFuzzy(candidate, query string) bool {
	queryTokens := tokenizeFuzzySearch(query)
	if len(queryTokens) == 0 {
		return true
	}
	candidateTokens := tokenizeFuzzySearch(candidate)
	if len(candidateTokens) == 0 {
		return false
	}

	candidateText := strings.Join(candidateTokens, " ")
	for _, token := range queryTokens {
		if !strings.Contains(candidateText, token) {
			return false
		}
	}
	return true
}

func (a *App) createPatientSession(ctx context.Context, patientID string, r *http.Request) (string, string, time.Time, error) {
	rawToken, err := randomToken(32)
	if err != nil {
		return "", "", time.Time{}, err
	}
	expiresAt := time.Now().UTC().Add(a.portalSessionDuration())
	var sessionID string
	err = a.db.QueryRowContext(ctx, `
		INSERT INTO patient_sessions (
			patient_id, status, verification_channel, verification_completed_at,
			expires_at, last_seen_at, client_ip, user_agent, token_hash
		) VALUES (
			$1::uuid, 'active', 'portal_login', now(),
			$2, now(), NULLIF($3, '')::inet, NULLIF($4, ''), $5
		)
		RETURNING id::text
	`, patientID, expiresAt, clientIPForRateLimit(r), strings.TrimSpace(r.UserAgent()), tokenHash(rawToken)).Scan(&sessionID)
	if err != nil {
		return "", "", time.Time{}, err
	}
	return sessionID, rawToken, expiresAt, nil
}

func (a *App) storePatientMailCode(ctx context.Context, patientID, code string, ttl time.Duration) error {
	patientID = strings.TrimSpace(patientID)
	code = strings.TrimSpace(code)
	if patientID == "" || code == "" {
		return errors.New("missing patient id or code")
	}
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	expiresAt := time.Now().UTC().Add(ttl)
	_, err := a.db.ExecContext(ctx, `
		INSERT INTO patient_mail_codes (patient_id, code_hash, expires_at, created_at)
		VALUES ($1::uuid, $2, $3, now())
	`, patientID, tokenHash(code), expiresAt)
	return err
}

func (a *App) consumePatientMailCode(ctx context.Context, patientID, code string) (bool, error) {
	patientID = strings.TrimSpace(patientID)
	code = strings.TrimSpace(code)
	if patientID == "" || code == "" {
		return false, nil
	}

	var consumedID string
	err := a.db.QueryRowContext(ctx, `
		WITH candidate AS (
			SELECT id
			FROM patient_mail_codes
			WHERE patient_id = $1::uuid
			  AND code_hash = $2
			  AND consumed_at IS NULL
			  AND expires_at > now()
			ORDER BY created_at DESC
			LIMIT 1
		)
		UPDATE patient_mail_codes
		SET consumed_at = now()
		WHERE id IN (SELECT id FROM candidate)
		RETURNING id::text
	`, patientID, tokenHash(code)).Scan(&consumedID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(consumedID) != "", nil
}

func (a *App) patientSessionByToken(ctx context.Context, rawToken string) (patientSessionSnapshot, error) {
	var session patientSessionSnapshot
	err := a.db.QueryRowContext(ctx, `
		SELECT id::text, patient_id::text, expires_at, status
		FROM patient_sessions
		WHERE token_hash = $1
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, tokenHash(rawToken)).Scan(&session.SessionID, &session.PatientID, &session.ExpiresAt, &session.Status)
	if err != nil {
		return patientSessionSnapshot{}, err
	}
	if session.Status != "active" || time.Now().UTC().After(session.ExpiresAt) {
		return patientSessionSnapshot{}, sql.ErrNoRows
	}
	_, _ = a.db.ExecContext(ctx, `UPDATE patient_sessions SET last_seen_at = now() WHERE id = $1::uuid`, session.SessionID)
	return session, nil
}

func (a *App) invalidatePatientSessionByToken(ctx context.Context, rawToken string) error {
	result, err := a.db.ExecContext(ctx, `
		UPDATE patient_sessions
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

func (a *App) requirePatientSessionSummary(ctx context.Context, r *http.Request) (patientSessionSnapshot, PatientSummary, error) {
	rawSessionToken := sessionCookieToken(r, patientSessionCookieName)
	if rawSessionToken == "" {
		return patientSessionSnapshot{}, PatientSummary{}, sql.ErrNoRows
	}

	session, err := a.patientSessionByToken(ctx, rawSessionToken)
	if err != nil {
		return patientSessionSnapshot{}, PatientSummary{}, err
	}

	patient, err := a.getPatientSummaryByID(ctx, session.PatientID)
	if err != nil {
		return patientSessionSnapshot{}, PatientSummary{}, err
	}

	return session, patient, nil
}

func (a *App) ensurePatientRecord(ctx context.Context, documentNumber string) (PatientSummary, error) {
	patient, _, err := a.ensurePatientRecordWithIdentity(ctx, documentNumber)
	if err != nil {
		return PatientSummary{}, fmt.Errorf("resolve patient identity via %s: %w", a.identitySource.ProviderName(), err)
	}
	return patient, nil
}

func (a *App) getPatientSummaryByID(ctx context.Context, patientID string) (PatientSummary, error) {
	var patient PatientSummary
	var birthDate sql.NullTime
	var genderIdentity sql.NullString
	err := a.db.QueryRowContext(ctx, `
		SELECT id::text, document_type, document_number, COALESCE(full_name, ''), birth_date, COALESCE(sex, ''), gender_identity
		FROM patients
		WHERE id = $1::uuid
	`, patientID).Scan(
		&patient.ID,
		&patient.DocumentType,
		&patient.DocumentNumber,
		&patient.FullName,
		&birthDate,
		&patient.Sex,
		&genderIdentity,
	)
	if err != nil {
		return PatientSummary{}, err
	}
	if birthDate.Valid {
		patient.BirthDate = birthDate.Time.UTC().Format("2006-01-02")
	}
	if genderIdentity.Valid {
		patient.GenderIdentity = strings.TrimSpace(genderIdentity.String)
	}
	return patient, nil
}

func (a *App) syncPatientStudiesFromSingleNode(ctx context.Context, patient PatientSummary, documentNumber string, filters PatientStudiesFilter) (PatientSummary, error) {
	nodes := make([]PACSNodeConfig, 0, len(a.externalConfig.PACSNodes))
	for _, node := range a.externalConfig.PACSNodes {
		searchMode := strings.ToLower(strings.TrimSpace(node.Resolved().SearchMode))
		if searchMode == "qido_rs" || searchMode == "c_find" {
			nodes = append(nodes, node)
		}
	}
	if len(nodes) == 0 {
		return patient, errors.New("patient remote flow requires at least one searchable pacs node")
	}
	sort.SliceStable(nodes, func(i, j int) bool {
		left := nodes[i].Resolved()
		right := nodes[j].Resolved()
		if left.Priority == right.Priority {
			return nodes[i].ID < nodes[j].ID
		}
		return left.Priority < right.Priority
	})

	syncStartedAt := time.Now()
	a.log("info", "patient_qido_sync_started", map[string]any{
		"document_number": documentNumber,
		"patient_id":      patient.ID,
		"node_count":      len(nodes),
		"sync_filters":    filters,
	})

	identifiers, err := a.loadPatientSearchIdentifiers(ctx, patient)
	if err != nil {
		return patient, err
	}
	if len(identifiers) == 0 {
		identifiers = []PatientAlternateIdentifier{{
			Type:      "document_number",
			Value:     patient.DocumentNumber,
			IsPrimary: true,
		}}
	}

	studyByUID := make(map[string]PatientStudy)
	successfulNodeCount := 0
	failedNodeCount := 0
	var lastErr error
	cacheCandidates := make([]PatientStudy, 0)

	for _, node := range nodes {
		var remoteStudies []PatientStudy
		switch strings.ToLower(strings.TrimSpace(node.Resolved().SearchMode)) {
		case "qido_rs":
			remoteStudies, _, err = a.fetchPatientStudiesFromQIDO(ctx, node, patient, filters, identifiers)
		case "c_find":
			remoteStudies, err = a.fetchPatientStudiesFromCFind(ctx, node, patient, filters, identifiers)
		default:
			continue
		}
		if err != nil {
			failedNodeCount++
			lastErr = err
			a.log("error", "patient_qido_node_failed", map[string]any{
				"document_number": documentNumber,
				"patient_id":      patient.ID,
				"node_id":         node.ID,
				"error":           err.Error(),
			})
			continue
		}
		cacheCandidates = append(cacheCandidates, remoteStudies...)
		successfulNodeCount++
		for _, study := range remoteStudies {
			existing, ok := studyByUID[study.StudyInstanceUID]
			if !ok {
				studyByUID[study.StudyInstanceUID] = study
				continue
			}
			if existing.StudyDate == "" && study.StudyDate != "" {
				existing.StudyDate = study.StudyDate
			}
			if existing.StudyDescription == "" && study.StudyDescription != "" {
				existing.StudyDescription = study.StudyDescription
			}
			if len(existing.ModalitiesInStudy) == 0 && len(study.ModalitiesInStudy) > 0 {
				existing.ModalitiesInStudy = study.ModalitiesInStudy
			}
			existing.Locations = mergeStringSets(existing.Locations, study.Locations)
			if existing.AuthorizationBasis == "" && study.AuthorizationBasis != "" {
				existing.AuthorizationBasis = study.AuthorizationBasis
			}
			if existing.SourceNodeID == "" && study.SourceNodeID != "" {
				existing.SourceNodeID = study.SourceNodeID
			}
			if existing.AvailabilityStatus != "available_local" && study.AvailabilityStatus == "available_local" {
				existing.AvailabilityStatus = study.AvailabilityStatus
				existing.ViewerURL = study.ViewerURL
				existing.OHIFViewerURL = study.OHIFViewerURL
				existing.DownloadURL = study.DownloadURL
			}
			studyByUID[study.StudyInstanceUID] = existing
		}
	}

	if successfulNodeCount == 0 {
		if lastErr == nil {
			lastErr = errors.New("patient qido search failed on all nodes")
		}
		return patient, lastErr
	}

	remoteStudies := make([]PatientStudy, 0, len(studyByUID))
	for _, study := range studyByUID {
		remoteStudies = append(remoteStudies, study)
	}
	sort.Slice(remoteStudies, func(i, j int) bool {
		if remoteStudies[i].StudyDate == remoteStudies[j].StudyDate {
			return remoteStudies[i].StudyInstanceUID < remoteStudies[j].StudyInstanceUID
		}
		return remoteStudies[i].StudyDate > remoteStudies[j].StudyDate
	})

	if err := a.enrichPatientStudiesWithAndes(ctx, patient.ID, remoteStudies); err != nil {
		a.log("error", "patient_andes_enrichment_failed", map[string]any{
			"patient_id": patient.ID,
			"error":      err.Error(),
		})
	}
	if err := a.persistPatientStudiesToQIDOCache(ctx, cacheCandidates, remoteStudies); err != nil {
		a.log("error", "patient_qido_cache_persist_failed", map[string]any{
			"patient_id": patient.ID,
			"error":      err.Error(),
		})
	}

	if err := a.replacePatientStudyAccessSlice(ctx, patient.ID, filters, remoteStudies); err != nil {
		return patient, err
	}

	availableLocalCount := 0
	for _, study := range remoteStudies {
		if study.ViewerURL != "" {
			availableLocalCount++
		}
	}

	a.log("info", "patient_qido_sync_completed", map[string]any{
		"document_number":     documentNumber,
		"patient_id":          patient.ID,
		"node_count":          len(nodes),
		"successful_nodes":    successfulNodeCount,
		"failed_nodes":        failedNodeCount,
		"studies_synced":      len(remoteStudies),
		"studies_local_ready": availableLocalCount,
		"duration_ms":         time.Since(syncStartedAt).Milliseconds(),
	})

	return patient, nil
}

func (a *App) loadPatientSearchIdentifiers(ctx context.Context, patient PatientSummary) ([]PatientAlternateIdentifier, error) {
	identifiers := make([]PatientAlternateIdentifier, 0, 2)
	seen := make(map[string]struct{})

	addIdentifier := func(identifierType, identifierValue string, isPrimary bool) {
		identifierType = strings.TrimSpace(identifierType)
		identifierValue = strings.TrimSpace(identifierValue)
		if identifierType == "" || identifierValue == "" {
			return
		}
		key := strings.ToLower(identifierType) + "\x00" + identifierValue
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		identifiers = append(identifiers, PatientAlternateIdentifier{
			Type:      identifierType,
			Value:     identifierValue,
			IsPrimary: isPrimary,
		})
	}

	addIdentifier("document_number", patient.DocumentNumber, true)

	rows, err := a.db.QueryContext(ctx, `
		SELECT identifier_type, identifier_value, is_primary
		FROM patient_identifiers
		WHERE patient_id = $1::uuid
		  AND identifier_type IN ('document_number', 'mongo_object_id')
		ORDER BY
			CASE identifier_type
				WHEN 'document_number' THEN 0
				WHEN 'mongo_object_id' THEN 1
				ELSE 9
			END,
			identifier_value ASC
	`, patient.ID)
	if err != nil {
		return nil, fmt.Errorf("load patient search identifiers: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			identifierType  string
			identifierValue string
			isPrimary       bool
		)
		if err := rows.Scan(&identifierType, &identifierValue, &isPrimary); err != nil {
			return nil, fmt.Errorf("scan patient search identifier: %w", err)
		}
		addIdentifier(identifierType, identifierValue, isPrimary)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate patient search identifiers: %w", err)
	}

	return identifiers, nil
}

func (a *App) processPatientSearchRequest(requestID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	var (
		patientID      string
		documentNumber string
		status         string
		queryJSONRaw   []byte
	)
	err := a.db.QueryRowContext(ctx, `
		SELECT sr.patient_id::text, p.document_number, sr.status, sr.query_json
		FROM search_requests sr
		JOIN patients p ON p.id = sr.patient_id
		WHERE sr.id = $1::uuid
		  AND sr.actor_type = 'patient'
	`, requestID).Scan(&patientID, &documentNumber, &status, &queryJSONRaw)
	if err != nil {
		a.log("error", "patient_search_load_failed", map[string]any{
			"request_id": requestID,
			"error":      err.Error(),
		})
		return
	}

	if status == "done" {
		return
	}

	var payload struct {
		DateFrom string `json:"date_from"`
		DateTo   string `json:"date_to"`
		Modality string `json:"modality"`
	}
	if len(queryJSONRaw) > 0 {
		if err := json.Unmarshal(queryJSONRaw, &payload); err != nil {
			a.log("error", "patient_search_decode_failed", map[string]any{
				"request_id": requestID,
				"error":      err.Error(),
			})
			return
		}
	}

	if _, err := a.db.ExecContext(ctx, `
		UPDATE search_requests
		SET status = 'running', finished_at = NULL
		WHERE id = $1::uuid
	`, requestID); err != nil {
		a.log("error", "patient_search_mark_running_failed", map[string]any{
			"request_id": requestID,
			"error":      err.Error(),
		})
		return
	}
	if _, err := a.db.ExecContext(ctx, `
		UPDATE search_node_runs
		SET status = 'running', started_at = now(), finished_at = NULL, error = NULL
		WHERE search_request_id = $1::uuid
	`, requestID); err != nil {
		a.log("error", "patient_search_node_running_failed", map[string]any{
			"request_id": requestID,
			"error":      err.Error(),
		})
		return
	}

	patient, err := a.getPatientSummaryByID(ctx, patientID)
	if err != nil {
		a.log("error", "patient_search_patient_load_failed", map[string]any{
			"request_id": requestID,
			"patient_id": patientID,
			"error":      err.Error(),
		})
		_, _ = a.db.ExecContext(ctx, `
			UPDATE search_requests
			SET status = 'failed', finished_at = now()
			WHERE id = $1::uuid
		`, requestID)
		_, _ = a.db.ExecContext(ctx, `
			UPDATE search_node_runs
			SET status = 'failed', finished_at = now(), error = $2
			WHERE search_request_id = $1::uuid
		`, requestID, err.Error())
		return
	}
	filters := PatientStudiesFilter{
		DateFrom: payload.DateFrom,
		DateTo:   payload.DateTo,
		Modality: strings.ToUpper(strings.TrimSpace(payload.Modality)),
	}

	startedAt := time.Now()
	if _, err := a.syncPatientStudiesFromSingleNode(ctx, patient, documentNumber, filters); err != nil {
		_, _ = a.db.ExecContext(ctx, `
			UPDATE search_requests
			SET status = 'failed', finished_at = now()
			WHERE id = $1::uuid
		`, requestID)
		_, _ = a.db.ExecContext(ctx, `
			UPDATE search_node_runs
			SET status = 'failed', finished_at = now(), error = $2
			WHERE search_request_id = $1::uuid
		`, requestID, err.Error())
		a.log("error", "patient_search_failed", map[string]any{
			"request_id": requestID,
			"patient_id": patientID,
			"error":      err.Error(),
		})
		return
	}

	latency := int(time.Since(startedAt).Milliseconds())
	_, _ = a.db.ExecContext(ctx, `
		UPDATE search_requests
		SET status = 'done', finished_at = now()
		WHERE id = $1::uuid
	`, requestID)
	_, _ = a.db.ExecContext(ctx, `
		UPDATE search_node_runs
		SET status = 'done', finished_at = now(), latency_ms = $2, error = NULL
		WHERE search_request_id = $1::uuid
	`, requestID, latency)
}

func (a *App) replacePatientStudyAccessSlice(ctx context.Context, patientID string, filters PatientStudiesFilter, studies []PatientStudy) error {
	if err := a.deletePatientStudyAccessSlice(ctx, patientID, filters); err != nil {
		return fmt.Errorf("clear patient study access slice: %w", err)
	}

	for _, study := range studies {
		sourceJSON, err := json.Marshal(map[string]any{
			"study_date":          study.StudyDate,
			"study_description":   study.StudyDescription,
			"number_of_images":    study.NumberOfImages,
			"modalities_in_study": study.ModalitiesInStudy,
			"locations":           study.Locations,
			"source_node_id":      study.SourceNodeID,
			"andes_prestacion_id": study.AndesPrestacionID,
			"andes_prestacion":    study.AndesPrestacion,
			"andes_professional":  study.AndesProfessional,
		})
		if err != nil {
			return fmt.Errorf("marshal patient qido study: %w", err)
		}

		availabilityStatus := "pending_retrieve"
		if study.ViewerURL != "" {
			availabilityStatus = "available_local"
		}

		if _, err := a.db.ExecContext(ctx, `
			INSERT INTO patient_study_access (
				patient_id, study_instance_uid, authorization_basis, availability_status,
				local_orthanc_study_id, first_seen_at, last_seen_at, last_authorized_at, source_json
			) VALUES (
				$1::uuid, $2, $3, $4, NULL, now(), now(), now(), $5::jsonb
			)
			ON CONFLICT (patient_id, study_instance_uid) DO UPDATE SET
				authorization_basis = EXCLUDED.authorization_basis,
				availability_status = EXCLUDED.availability_status,
				last_seen_at = now(),
				last_authorized_at = now(),
				source_json = EXCLUDED.source_json
		`,
			patientID,
			study.StudyInstanceUID,
			study.AuthorizationBasis,
			availabilityStatus,
			string(sourceJSON),
		); err != nil {
			return fmt.Errorf("insert qido-backed patient study access: %w", err)
		}
	}

	return nil
}

func (a *App) deletePatientStudyAccessSlice(ctx context.Context, patientID string, filters PatientStudiesFilter) error {
	query := `
		DELETE FROM patient_study_access
		WHERE patient_id = $1::uuid
	`
	args := []any{patientID}
	position := 2

	if filters.DateFrom != "" {
		query += fmt.Sprintf(` AND REPLACE(COALESCE(source_json->>'study_date', ''), '-', '') >= REPLACE($%d, '-', '')`, position)
		args = append(args, filters.DateFrom)
		position++
	}
	if filters.DateTo != "" {
		query += fmt.Sprintf(` AND REPLACE(COALESCE(source_json->>'study_date', ''), '-', '') <= REPLACE($%d, '-', '')`, position)
		args = append(args, filters.DateTo)
		position++
	}
	if filters.Modality != "" {
		query += fmt.Sprintf(` AND EXISTS (
			SELECT 1
			FROM jsonb_array_elements_text(COALESCE(source_json->'modalities_in_study', '[]'::jsonb)) AS modality
			WHERE UPPER(modality) = $%d
		)`, position)
		args = append(args, filters.Modality)
		position++
	}

	_, err := a.db.ExecContext(ctx, query, args...)
	return err
}

func (a *App) queuePatientRetrieve(ctx context.Context, patient PatientSummary, studyInstanceUID string) (PatientRetrieveResponse, error) {
	activeJob, err := a.findActiveRetrieveJobByStudy(ctx, studyInstanceUID)
	if err != nil {
		return PatientRetrieveResponse{}, err
	}
	if activeJob != nil {
		return PatientRetrieveResponse{
			JobID:            activeJob.JobID,
			StudyInstanceUID: activeJob.StudyInstanceUID,
			Status:           activeJob.Status,
		}, nil
	}

	_, sourceNodeID, err := a.getPatientSourceNode(ctx, patient.ID, studyInstanceUID)
	if err != nil {
		return PatientRetrieveResponse{}, err
	}
	if !a.sourceNodeAvailable(sourceNodeID) {
		return PatientRetrieveResponse{}, fmt.Errorf("%w: %s", ErrSourceNodeUnavailable, sourceNodeID)
	}
	modalities, err := a.patientStudyModalities(ctx, patient.ID, studyInstanceUID)
	if err != nil {
		return PatientRetrieveResponse{}, err
	}
	if blocked := a.blockedRetrieveModality(modalities); blocked != "" {
		return PatientRetrieveResponse{}, fmt.Errorf("retrieve blocked for modality %s", blocked)
	}

	jobID, err := a.insertRetrieveJob(ctx, studyInstanceUID, sourceNodeID, "patient", patient.ID)
	if err != nil {
		return PatientRetrieveResponse{}, fmt.Errorf("insert retrieve job: %w", err)
	}
	a.log("info", "patient_retrieve_queued", map[string]any{
		"patient_id":         patient.ID,
		"study_instance_uid": studyInstanceUID,
		"source_node_id":     sourceNodeID,
		"job_id":             jobID,
	})
	a.enqueueRetrieveJob(jobID)

	return PatientRetrieveResponse{
		JobID:            jobID,
		StudyInstanceUID: studyInstanceUID,
		Status:           "queued",
	}, nil
}

func (a *App) fetchPatientStudiesFromQIDO(ctx context.Context, node PACSNodeConfig, patient PatientSummary, filters PatientStudiesFilter, identifiers []PatientAlternateIdentifier) ([]PatientStudy, string, error) {
	resolved := node.Resolved()
	qidoStartedAt := time.Now()
	token, err := a.fetchPACSBearerToken(ctx, node)
	if err != nil {
		return nil, "", fmt.Errorf("fetch pacs token for %s: %w", node.ID, err)
	}
	if len(identifiers) == 0 {
		identifiers = []PatientAlternateIdentifier{{
			Type:      "document_number",
			Value:     patient.DocumentNumber,
			IsPrimary: true,
		}}
	}

	patientName := ""
	studyByUID := make(map[string]PatientStudy)
	for _, identifier := range identifiers {
		identifierStudies, identifierPatientName, err := a.fetchPatientStudiesFromQIDOIdentifier(ctx, node, resolved, token, patient, identifier, filters)
		if err != nil {
			return nil, "", err
		}
		if patientName == "" && identifierPatientName != "" {
			patientName = identifierPatientName
		}
		for _, study := range identifierStudies {
			existing, ok := studyByUID[study.StudyInstanceUID]
			if !ok {
				studyByUID[study.StudyInstanceUID] = study
				continue
			}
			if existing.StudyDate == "" && study.StudyDate != "" {
				existing.StudyDate = study.StudyDate
			}
			if existing.StudyDescription == "" && study.StudyDescription != "" {
				existing.StudyDescription = study.StudyDescription
			}
			if len(existing.ModalitiesInStudy) == 0 && len(study.ModalitiesInStudy) > 0 {
				existing.ModalitiesInStudy = study.ModalitiesInStudy
			}
			existing.Locations = mergeStringSets(existing.Locations, study.Locations)
			if existing.AvailabilityStatus != "available_local" && study.AvailabilityStatus == "available_local" {
				existing.AvailabilityStatus = study.AvailabilityStatus
				existing.ViewerURL = study.ViewerURL
				existing.OHIFViewerURL = study.OHIFViewerURL
				existing.DownloadURL = study.DownloadURL
			}
			studyByUID[study.StudyInstanceUID] = existing
		}
	}

	studies := make([]PatientStudy, 0, len(studyByUID))
	for _, study := range studyByUID {
		studies = append(studies, study)
	}

	sort.Slice(studies, func(i, j int) bool {
		if studies[i].StudyDate == studies[j].StudyDate {
			return studies[i].StudyInstanceUID < studies[j].StudyInstanceUID
		}
		return studies[i].StudyDate > studies[j].StudyDate
	})

	a.log("info", "patient_qido_request_completed", map[string]any{
		"document_number":  patient.DocumentNumber,
		"node_id":          node.ID,
		"study_count":      len(studies),
		"identifier_count": len(identifiers),
		"duration_ms":      time.Since(qidoStartedAt).Milliseconds(),
	})

	return studies, patientName, nil
}

func (a *App) fetchPatientStudiesFromQIDOIdentifier(ctx context.Context, node PACSNodeConfig, resolved PACSNodeResolvedConfig, token string, patient PatientSummary, identifier PatientAlternateIdentifier, filters PatientStudiesFilter) ([]PatientStudy, string, error) {
	endpoint, err := url.Parse(strings.TrimRight(resolved.DICOMwebBaseURL, "/") + "/studies")
	if err != nil {
		return nil, "", fmt.Errorf("build qido url: %w", err)
	}

	query := endpoint.Query()
	query.Set("PatientID", strings.TrimSpace(identifier.Value))
	if filters.DateFrom != "" || filters.DateTo != "" {
		query.Set("StudyDate", buildQIDODateRange(filters.DateFrom, filters.DateTo))
	}
	query.Set("limit", "50")
	query.Add("includefield", "StudyInstanceUID")
	query.Add("includefield", "StudyDate")
	query.Add("includefield", "StudyDescription")
	query.Add("includefield", "ModalitiesInStudy")
	query.Add("includefield", "PatientName")
	query.Add("includefield", "AccessionNumber")
	query.Add("includefield", "NumberOfStudyRelatedInstances")
	endpoint.RawQuery = query.Encode()

	a.log("info", "patient_qido_request_started", map[string]any{
		"document_number":  patient.DocumentNumber,
		"patient_id_type":  identifier.Type,
		"patient_id_value": identifier.Value,
		"node_id":          node.ID,
		"url":              endpoint.String(),
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, "", fmt.Errorf("build qido request: %w", err)
	}
	req.Header.Set("Accept", "application/dicom+json, application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	res, err := a.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("execute qido request: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return nil, "", fmt.Errorf("qido bad status %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload []qidoResponseItem
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		if errors.Is(err, io.EOF) {
			payload = []qidoResponseItem{}
		} else {
			return nil, "", fmt.Errorf("decode qido response: %w", err)
		}
	}

	studies := make([]PatientStudy, 0, len(payload))
	patientName := ""
	authorizationBasis := "patient_identifier_qido_match"
	if identifier.Type == "document_number" {
		authorizationBasis = "patient_document_qido_match"
	}

	for _, item := range payload {
		studyUID := dicomFirstString(item, "0020000D")
		if studyUID == "" {
			continue
		}
		a.logAccessionNumberProbe("patient_remote_qido", node.ID, studyUID, dicomFirstString(item, "00080050"))
		a.logPatientIdentityComparison(patient, remotePatientMatchCandidate{
			NodeID:           node.ID,
			StudyInstanceUID: studyUID,
			PatientID:        dicomFirstString(item, "00100020"),
			PatientName:      dicomFirstPersonName(item, "00100010"),
		})

		study := PatientStudy{
			StudyInstanceUID:   studyUID,
			StudyDate:          normalizeStudyDate(dicomFirstString(item, "00080020")),
			StudyDescription:   dicomFirstString(item, "00081030"),
			NumberOfImages:     dicomFirstInt(item, "00201208"),
			ModalitiesInStudy:  dicomStringList(item, "00080061"),
			Locations:          []string{node.Name},
			AvailabilityStatus: "pending_retrieve",
			AuthorizationBasis: authorizationBasis,
			HIS:                node.HIS,
			SourceNodeID:       node.ID,
		}

		cached, err := a.isStudyAvailableLocal(ctx, studyUID)
		if err != nil {
			return nil, "", fmt.Errorf("check local cache for study %s: %w", studyUID, err)
		}
		if cached {
			study.AvailabilityStatus = "available_local"
			study.ViewerURL = buildStoneViewerURL(studyUID)
			study.OHIFViewerURL = buildOHIFViewerURL(studyUID)
			study.DownloadURL = buildPatientDownloadURL(studyUID)
		}

		if patientName == "" {
			patientName = dicomFirstPersonName(item, "00100010")
		}

		studies = append(studies, study)
	}

	return studies, patientName, nil
}

func (a *App) fetchPatientStudiesFromCFind(ctx context.Context, node PACSNodeConfig, patient PatientSummary, filters PatientStudiesFilter, identifiers []PatientAlternateIdentifier) ([]PatientStudy, error) {
	observedStudies := make(map[string]struct{})
	studyByUID := make(map[string]PatientStudy)
	identifierIndex := patientIdentifierSet(identifiers)
	collectAuthorizedStudy := func(item qidoResponseItem) error {
		studyUID := dicomFirstString(item, "0020000D")
		if studyUID == "" {
			return nil
		}
		observedStudies[studyUID] = struct{}{}
		remotePatientID := dicomFirstString(item, "00100020")
		candidate := remotePatientMatchCandidate{
			NodeID:           node.ID,
			StudyInstanceUID: studyUID,
			PatientID:        remotePatientID,
			PatientName:      dicomFirstPersonName(item, "00100010"),
			BirthDate:        dicomFirstString(item, "00100030"),
			Sex:              dicomFirstString(item, "00100040"),
		}
		a.logPatientIdentityComparison(patient, candidate)

		authorizationBasis := ""
		if matchedIdentifier, ok := identifierIndex[strings.TrimSpace(remotePatientID)]; ok {
			authorizationBasis = "patient_identifier_cfind_match"
			if matchedIdentifier.Type == "document_number" {
				authorizationBasis = "patient_document_cfind_match"
			}
		} else if patientDemographicMatch(patient, candidate) {
			authorizationBasis = "patient_demographic_cfind_match"
		}
		if authorizationBasis == "" {
			return nil
		}
		a.logAccessionNumberProbe("patient_remote_cfind", node.ID, studyUID, dicomFirstString(item, "00080050"))

		study := PatientStudy{
			StudyInstanceUID:   studyUID,
			StudyDate:          normalizeStudyDate(dicomFirstString(item, "00080020")),
			StudyDescription:   dicomFirstString(item, "00081030"),
			NumberOfImages:     dicomFirstInt(item, "00201208"),
			ModalitiesInStudy:  dicomStringList(item, "00080061"),
			Locations:          []string{node.Name},
			AvailabilityStatus: "pending_retrieve",
			AuthorizationBasis: authorizationBasis,
			HIS:                node.HIS,
			SourceNodeID:       node.ID,
		}

		cached, err := a.isStudyAvailableLocal(ctx, studyUID)
		if err != nil {
			return fmt.Errorf("check local cache for study %s: %w", studyUID, err)
		}
		if cached {
			study.AvailabilityStatus = "available_local"
			study.ViewerURL = buildStoneViewerURL(studyUID)
			study.OHIFViewerURL = buildOHIFViewerURL(studyUID)
			study.DownloadURL = buildPatientDownloadURL(studyUID)
		}

		existing, ok := studyByUID[studyUID]
		if !ok {
			studyByUID[studyUID] = study
			return nil
		}
		if existing.StudyDate == "" && study.StudyDate != "" {
			existing.StudyDate = study.StudyDate
		}
		if existing.StudyDescription == "" && study.StudyDescription != "" {
			existing.StudyDescription = study.StudyDescription
		}
		if existing.NumberOfImages == 0 && study.NumberOfImages > 0 {
			existing.NumberOfImages = study.NumberOfImages
		}
		if len(existing.ModalitiesInStudy) == 0 && len(study.ModalitiesInStudy) > 0 {
			existing.ModalitiesInStudy = study.ModalitiesInStudy
		}
		if len(existing.Locations) == 0 && len(study.Locations) > 0 {
			existing.Locations = study.Locations
		}
		if existing.AuthorizationBasis == "" && study.AuthorizationBasis != "" {
			existing.AuthorizationBasis = study.AuthorizationBasis
		}
		if existing.SourceNodeID == "" && study.SourceNodeID != "" {
			existing.SourceNodeID = study.SourceNodeID
		}
		if existing.AvailabilityStatus != "available_local" && study.AvailabilityStatus == "available_local" {
			existing.AvailabilityStatus = study.AvailabilityStatus
			existing.ViewerURL = study.ViewerURL
			existing.OHIFViewerURL = study.OHIFViewerURL
			existing.DownloadURL = study.DownloadURL
		}
		studyByUID[studyUID] = existing
		return nil
	}

	for _, identifier := range identifiers {
		identifierValue := strings.TrimSpace(identifier.Value)
		if identifierValue == "" {
			continue
		}
		payload, err := a.runOrthancStudyCFind(ctx, node, PhysicianSearchFilters{
			PatientID: identifierValue,
			DateFrom:  filters.DateFrom,
			DateTo:    filters.DateTo,
			Modality:  filters.Modality,
		})
		if err != nil {
			return nil, fmt.Errorf("run patient c-find on %s with %s=%s: %w", node.ID, identifier.Type, identifierValue, err)
		}

		for _, item := range payload {
			if err := collectAuthorizedStudy(item); err != nil {
				return nil, err
			}
		}
	}

	demographicBirthDate := formatDICOMDate(patient.BirthDate)
	demographicSex := normalizeRemoteSex(patient.Sex)
	demographicNameQuery := buildPatientSurnameCFindQuery(patient.FullName)
	if demographicNameQuery != "" && demographicBirthDate != "" && demographicSex != "" {
		payload, err := a.runOrthancStudyCFind(ctx, node, PhysicianSearchFilters{
			PatientNameRaw: demographicNameQuery,
			BirthDate:      demographicBirthDate,
			Sex:            demographicSex,
			DateFrom:       filters.DateFrom,
			DateTo:         filters.DateTo,
			Modality:       filters.Modality,
		})
		if err != nil {
			return nil, fmt.Errorf("run patient demographic c-find on %s: %w", node.ID, err)
		}
		for _, item := range payload {
			if err := collectAuthorizedStudy(item); err != nil {
				return nil, err
			}
		}
	}

	studies := make([]PatientStudy, 0, len(studyByUID))
	for _, study := range studyByUID {
		studies = append(studies, study)
	}
	sort.Slice(studies, func(i, j int) bool {
		if studies[i].StudyDate == studies[j].StudyDate {
			return studies[i].StudyInstanceUID < studies[j].StudyInstanceUID
		}
		return studies[i].StudyDate > studies[j].StudyDate
	})

	return studies, nil
}

func (a *App) patientStudyAvailableLocal(ctx context.Context, patientID, studyUID string) (bool, error) {
	var availabilityStatus string
	err := a.db.QueryRowContext(ctx, `
		SELECT availability_status
		FROM patient_study_access
		WHERE patient_id = $1::uuid
		  AND study_instance_uid = $2
	`, patientID, studyUID).Scan(&availabilityStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return availabilityStatus == "available_local", nil
}

func (a *App) patientStudyAccessible(ctx context.Context, patientID, studyUID string) (bool, error) {
	var exists bool
	err := a.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM patient_study_access
			WHERE patient_id = $1::uuid
			  AND study_instance_uid = $2
		)
	`, patientID, studyUID).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

func (a *App) patientStudyModalities(ctx context.Context, patientID, studyInstanceUID string) ([]string, error) {
	var raw []byte
	err := a.db.QueryRowContext(ctx, `
		SELECT source_json
		FROM patient_study_access
		WHERE patient_id = $1::uuid
		  AND study_instance_uid = $2
	`, patientID, studyInstanceUID).Scan(&raw)
	if err != nil {
		return nil, err
	}

	var source struct {
		ModalitiesInStudy []string `json:"modalities_in_study"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &source); err != nil {
			return nil, fmt.Errorf("parse patient retrieve source_json: %w", err)
		}
	}
	return source.ModalitiesInStudy, nil
}

func (a *App) getPatientSourceNode(ctx context.Context, patientID, studyInstanceUID string) (PACSNodeConfig, string, error) {
	var sourceJSONRaw []byte
	if err := a.db.QueryRowContext(ctx, `
		SELECT source_json
		FROM patient_study_access
		WHERE patient_id = $1::uuid
		  AND study_instance_uid = $2
	`, patientID, studyInstanceUID).Scan(&sourceJSONRaw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return PACSNodeConfig{}, "", errors.New("patient study not found")
		}
		return PACSNodeConfig{}, "", err
	}

	var source struct {
		SourceNodeID string `json:"source_node_id"`
	}
	_ = json.Unmarshal(sourceJSONRaw, &source)
	if source.SourceNodeID == "" {
		if len(a.externalConfig.PACSNodes) == 1 {
			return a.externalConfig.PACSNodes[0], a.externalConfig.PACSNodes[0].ID, nil
		}
		return PACSNodeConfig{}, "", errors.New("source node id missing for patient study")
	}

	for _, node := range a.externalConfig.PACSNodes {
		if node.ID == source.SourceNodeID {
			return node, source.SourceNodeID, nil
		}
	}

	return PACSNodeConfig{}, "", fmt.Errorf("unknown source node id %q", source.SourceNodeID)
}

func (a *App) markPatientStudyAvailableLocal(ctx context.Context, patientID, studyUID, orthancStudyID, sourceNodeID string) error {
	_, err := a.db.ExecContext(ctx, `
		UPDATE patient_study_access
		SET availability_status = 'available_local',
		    local_orthanc_study_id = $3,
		    last_seen_at = now(),
		    last_authorized_at = now(),
		    source_json = jsonb_set(
		      jsonb_set(COALESCE(source_json, '{}'::jsonb), '{source_node_id}', to_jsonb($4::text), true),
		      '{orthanc_study_id}', to_jsonb($3::text), true
		    )
		WHERE patient_id = $1::uuid
		  AND study_instance_uid = $2
	`, patientID, studyUID, orthancStudyID, sourceNodeID)
	return err
}

func uniqueStudyUIDsFromPatientStudies(studies []PatientStudy) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(studies))
	for _, study := range studies {
		uid := strings.TrimSpace(study.StudyInstanceUID)
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

func (a *App) applyPersistedQIDOCacheToPatientStudies(ctx context.Context, studies []PatientStudy) error {
	grouped := make(map[string][]string)
	for _, study := range studies {
		sourceNodeID := strings.TrimSpace(study.SourceNodeID)
		if sourceNodeID == "" {
			continue
		}
		grouped[sourceNodeID] = append(grouped[sourceNodeID], study.StudyInstanceUID)
	}

	cacheByNode := make(map[string]map[string]PersistedQIDOStudy, len(grouped))
	for sourceNodeID, studyUIDs := range grouped {
		cache, err := a.loadPersistedQIDOStudies(ctx, sourceNodeID, studyUIDs)
		if err != nil {
			return err
		}
		cacheByNode[sourceNodeID] = cache
	}

	for i := range studies {
		cache := cacheByNode[strings.TrimSpace(studies[i].SourceNodeID)]
		persisted, ok := cache[studies[i].StudyInstanceUID]
		if !ok {
			continue
		}
		if studies[i].AndesPrestacionID == "" {
			studies[i].AndesPrestacionID = persisted.AndesPrestacionID
		}
		if studies[i].AndesPrestacion == "" {
			studies[i].AndesPrestacion = persisted.AndesPrestacion
		}
		if studies[i].AndesProfessional == "" {
			studies[i].AndesProfessional = persisted.AndesProfessional
		}
	}

	return nil
}

func (a *App) persistPatientStudiesToQIDOCache(ctx context.Context, rawStudies []PatientStudy, mergedStudies []PatientStudy) error {
	if len(rawStudies) == 0 {
		return nil
	}

	enrichmentByUID := make(map[string]PatientStudy, len(mergedStudies))
	for _, study := range mergedStudies {
		enrichmentByUID[study.StudyInstanceUID] = study
	}

	cacheEntries := make([]PersistedQIDOStudy, 0, len(rawStudies))
	for _, study := range rawStudies {
		enriched := enrichmentByUID[study.StudyInstanceUID]
		cacheEntries = append(cacheEntries, PersistedQIDOStudy{
			StudyInstanceUID:  study.StudyInstanceUID,
			SourceNodeID:      study.SourceNodeID,
			StudyDate:         study.StudyDate,
			StudyDescription:  study.StudyDescription,
			NumberOfImages:    study.NumberOfImages,
			Modalities:        study.ModalitiesInStudy,
			Locations:         study.Locations,
			AndesPrestacionID: enriched.AndesPrestacionID,
			AndesPrestacion:   enriched.AndesPrestacion,
			AndesProfessional: enriched.AndesProfessional,
		})
	}

	return a.persistQIDOStudies(ctx, cacheEntries)
}

func (a *App) listPatientStudies(ctx context.Context, patientID, documentNumber string, filters PatientStudiesFilter) ([]PatientStudy, error) {
	query := `
		SELECT
			study_instance_uid,
			availability_status,
			authorization_basis,
			source_json
		FROM patient_study_access
		WHERE patient_id = $1::uuid
	`

	args := []any{patientID}
	position := 2

	if filters.DateFrom != "" {
		query += fmt.Sprintf(` AND REPLACE(COALESCE(source_json->>'study_date', ''), '-', '') >= REPLACE($%d, '-', '')`, position)
		args = append(args, filters.DateFrom)
		position++
	}
	if filters.DateTo != "" {
		query += fmt.Sprintf(` AND REPLACE(COALESCE(source_json->>'study_date', ''), '-', '') <= REPLACE($%d, '-', '')`, position)
		args = append(args, filters.DateTo)
		position++
	}
	if filters.Modality != "" {
		query += fmt.Sprintf(` AND EXISTS (
			SELECT 1
			FROM jsonb_array_elements_text(COALESCE(source_json->'modalities_in_study', '[]'::jsonb)) AS modality
			WHERE UPPER(modality) = $%d
		)`, position)
		args = append(args, filters.Modality)
		position++
	}

	query += ` ORDER BY COALESCE(source_json->>'study_date', '') DESC, study_instance_uid ASC`

	rows, err := a.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	studies := make([]PatientStudy, 0)
	for rows.Next() {
		var (
			studyUID           string
			availabilityStatus string
			authorizationBasis string
			sourceJSONRaw      []byte
		)

		if err := rows.Scan(&studyUID, &availabilityStatus, &authorizationBasis, &sourceJSONRaw); err != nil {
			return nil, err
		}

		var source struct {
			StudyDate         string   `json:"study_date"`
			StudyDescription  string   `json:"study_description"`
			NumberOfImages    int      `json:"number_of_images"`
			ModalitiesInStudy []string `json:"modalities_in_study"`
			Locations         []string `json:"locations"`
			SourceNodeID      string   `json:"source_node_id"`
			AndesPrestacionID string   `json:"andes_prestacion_id"`
			AndesPrestacion   string   `json:"andes_prestacion"`
			AndesProfessional string   `json:"andes_professional"`
		}
		if len(sourceJSONRaw) > 0 {
			if err := json.Unmarshal(sourceJSONRaw, &source); err != nil {
				return nil, fmt.Errorf("parse patient study source_json: %w", err)
			}
		}

		study := PatientStudy{
			StudyInstanceUID:    studyUID,
			StudyDate:           source.StudyDate,
			StudyDescription:    source.StudyDescription,
			NumberOfImages:      source.NumberOfImages,
			ModalitiesInStudy:   source.ModalitiesInStudy,
			Locations:           a.resolveLocationLabels(source.Locations),
			AndesPrestacionID:   source.AndesPrestacionID,
			AndesPrestacion:     source.AndesPrestacion,
			AndesProfessional:   source.AndesProfessional,
			AvailabilityStatus:  availabilityStatus,
			RetrieveStatus:      "idle",
			AuthorizationBasis:  authorizationBasis,
			HIS:                 a.andesMetadataAvailableForSourceNode(source.SourceNodeID),
			SourceNodeAvailable: a.sourceNodeAvailable(source.SourceNodeID),
			SourceNodeID:        source.SourceNodeID,
		}
		if len(study.Locations) == 0 && study.SourceNodeID != "" {
			study.Locations = []string{a.nodeDisplayName(study.SourceNodeID)}
		}
		cacheStatus := "not_local"
		if availabilityStatus == "available_local" {
			cacheStatus = "local_complete"
		}
		_, retrieveStatus, retrievePhase, retrieveProgress, viewerURL, ohifViewerURL, err := a.getStudyOperationalState(ctx, studyUID, cacheStatus, study.RetrieveStatus)
		if err != nil {
			return nil, fmt.Errorf("resolve patient study operational state for %s: %w", studyUID, err)
		}
		study.RetrieveStatus = retrieveStatus
		study.RetrievePhase = retrievePhase
		study.RetrieveProgress = retrieveProgress
		study.ViewerURL = viewerURL
		study.OHIFViewerURL = ohifViewerURL
		if viewerURL != "" {
			study.DownloadURL = buildPatientDownloadURL(studyUID)
		}

		studies = append(studies, study)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return studies, nil
}

func buildPatientDownloadURL(studyInstanceUID string) string {
	return "/api/patient/download?study_instance_uid=" + url.QueryEscape(strings.TrimSpace(studyInstanceUID))
}
