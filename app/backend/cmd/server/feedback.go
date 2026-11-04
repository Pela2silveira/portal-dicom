package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// feedback.go implements a lightweight "leave a comment" feature open to any
// portal user. Injection safety has two layers: all DB access is parameterized
// (no string building), and the stored message is sanitized server-side
// (control-char stripping + length cap). Rendering always HTML-escapes, so the
// message never reaches the DOM as markup.

const (
	feedbackMessageMaxLen   = 2000
	feedbackStudySiteMaxLen = 160
	feedbackPhoneMaxLen     = 30
	feedbackDefaultLimit    = 50
	feedbackMaxLimit        = 200
	feedbackActorNameMaxLen = 200
)

type feedbackCreateRequest struct {
	StudySite    string `json:"study_site"`
	ContactPhone string `json:"contact_phone"`
	Message      string `json:"message"`
}

type feedbackComment struct {
	ID           int64  `json:"id"`
	StudySite    string `json:"study_site,omitempty"`
	ContactPhone string `json:"contact_phone,omitempty"`
	Message      string `json:"message"`
	ActorKind    string `json:"actor_kind"`
	ActorID      string `json:"actor_id,omitempty"`
	ActorRole    string `json:"actor_role,omitempty"`
	ActorName    string `json:"actor_name,omitempty"`
	// DNI is the author's document number (patients.document_number or
	// physicians.dni), resolved from actor_id at list time.
	DNI string `json:"dni,omitempty"`
	// Contact is the author's email when known (patient email from
	// patient_identifiers) or the physician login (physicians.username) as a
	// fallback, since physicians have no email column.
	Contact   string    `json:"contact,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type feedbackListResponse struct {
	Limit    int               `json:"limit"`
	Comments []feedbackComment `json:"comments"`
}

// normalizeFeedbackStudySite keeps a single-line place name. Empty is allowed.
func normalizeFeedbackStudySite(raw string) string {
	cleaned := sanitizeFeedbackMessage(strings.ReplaceAll(raw, "\n", " "))
	cleaned = strings.Join(strings.Fields(cleaned), " ")
	if utf8.RuneCountInString(cleaned) > feedbackStudySiteMaxLen {
		cleaned = strings.TrimSpace(string([]rune(cleaned)[:feedbackStudySiteMaxLen]))
	}
	return cleaned
}

// normalizeFeedbackPhone keeps an optional contact number. Empty is allowed.
// A non-empty value must contain between 6 and 15 digits and no letters.
func normalizeFeedbackPhone(raw string) (string, bool) {
	cleaned := strings.Join(strings.Fields(sanitizeFeedbackMessage(raw)), " ")
	if cleaned == "" {
		return "", true
	}
	if utf8.RuneCountInString(cleaned) > feedbackPhoneMaxLen {
		return "", false
	}
	digits := 0
	var b strings.Builder
	for _, r := range cleaned {
		switch {
		case r >= '0' && r <= '9':
			digits++
			b.WriteRune(r)
		case r == '+' || r == '-' || r == ' ' || r == '(' || r == ')':
			b.WriteRune(r)
		default:
			return "", false
		}
	}
	if digits < 6 || digits > 15 {
		return "", false
	}
	return strings.TrimSpace(b.String()), true
}

// sanitizeFeedbackMessage normalizes user-submitted text: trims surrounding
// whitespace, drops control characters (except newlines/tabs) and invalid
// UTF-8, and caps the length (rune-aware). It returns the cleaned string; the
// caller rejects empty results.

func sanitizeFeedbackMessage(raw string) string {
	if !utf8.ValidString(raw) {
		raw = strings.ToValidUTF8(raw, "")
	}
	var b strings.Builder
	for _, r := range raw {
		// Keep printable runes plus newline and tab; drop other control chars.
		if r == '\n' || r == '\t' {
			b.WriteRune(r)
			continue
		}
		if r < 0x20 || r == 0x7f {
			continue
		}
		b.WriteRune(r)
	}
	cleaned := strings.TrimSpace(b.String())
	if utf8.RuneCountInString(cleaned) > feedbackMessageMaxLen {
		runes := []rune(cleaned)
		cleaned = strings.TrimSpace(string(runes[:feedbackMessageMaxLen]))
	}
	return cleaned
}

func truncateRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max])
}

func (a *App) handleFeedbackCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if a.db == nil {
		http.Error(w, "feedback store unavailable", http.StatusServiceUnavailable)
		return
	}

	var req feedbackCreateRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}

	studySite := normalizeFeedbackStudySite(req.StudySite)
	contactPhone, ok := normalizeFeedbackPhone(req.ContactPhone)
	if !ok {
		http.Error(w, "invalid phone", http.StatusBadRequest)
		return
	}
	message := sanitizeFeedbackMessage(req.Message)
	if message == "" {
		http.Error(w, "message is required", http.StatusBadRequest)
		return
	}

	actor, _ := actorFromContext(r.Context())
	actorName := truncateRunes(strings.TrimSpace(actor.DisplayName), feedbackActorNameMaxLen)

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var id int64
	if err := a.db.QueryRowContext(ctx, `
		INSERT INTO feedback_comments (study_site, contact_phone, message, actor_kind, actor_id, actor_role, actor_name, client_ip)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id
	`,
		nullIfEmpty(studySite),
		nullIfEmpty(contactPhone),
		message,
		string(actor.Kind),
		nullIfEmpty(actor.ID),
		nullIfEmpty(actor.Role),
		nullIfEmpty(actorName),
		nullIfEmpty(clientIPForRateLimit(r)),
	).Scan(&id); err != nil {
		a.log("error", "feedback_insert_failed", map[string]any{"error": err.Error()})
		http.Error(w, "failed to save comment", http.StatusInternalServerError)
		return
	}

	setActionDim(r.Context(), "feedback_id", id)
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "status": "saved"})
}

func parseFeedbackLimit(raw string) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n <= 0 {
		return feedbackDefaultLimit
	}
	if n > feedbackMaxLimit {
		return feedbackMaxLimit
	}
	return n
}

func (a *App) handleFeedbackList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if a.db == nil {
		http.Error(w, "feedback store unavailable", http.StatusServiceUnavailable)
		return
	}

	limit := parseFeedbackLimit(r.URL.Query().Get("limit"))

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// actor_id is compared as text (p.id::text = fc.actor_id) to avoid a uuid
	// cast on any malformed/empty actor_id. Email is patient-only (physicians
	// have no email column), so physicians fall back to their login username.
	rows, err := a.db.QueryContext(ctx, `
		SELECT fc.id,
		       COALESCE(fc.study_site, ''),
		       COALESCE(fc.contact_phone, ''),
		       fc.message,
		       fc.actor_kind,
		       COALESCE(fc.actor_id, '')   AS actor_id,
		       COALESCE(fc.actor_role, '') AS actor_role,
		       COALESCE(fc.actor_name, '') AS actor_name,
		       COALESCE(p.document_number, ph.dni, '')        AS dni,
		       COALESCE(pi.identifier_value, ph.username, '') AS contact,
		       fc.created_at
		FROM feedback_comments fc
		LEFT JOIN patients p
		  ON fc.actor_kind = 'patient' AND p.id::text = fc.actor_id
		LEFT JOIN physicians ph
		  ON fc.actor_kind = 'physician' AND ph.id::text = fc.actor_id
		LEFT JOIN LATERAL (
		  SELECT identifier_value
		  FROM patient_identifiers
		  WHERE patient_id = p.id AND identifier_type = 'email'
		  ORDER BY is_primary DESC, last_verified_at DESC NULLS LAST, updated_at DESC
		  LIMIT 1
		) pi ON fc.actor_kind = 'patient'
		ORDER BY fc.created_at DESC, fc.id DESC
		LIMIT $1
	`, limit)
	if err != nil {
		a.log("error", "feedback_list_query_failed", map[string]any{"error": err.Error()})
		http.Error(w, "failed to load comments", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	comments := []feedbackComment{}
	for rows.Next() {
		var c feedbackComment
		if err := rows.Scan(&c.ID, &c.StudySite, &c.ContactPhone, &c.Message, &c.ActorKind, &c.ActorID, &c.ActorRole, &c.ActorName, &c.DNI, &c.Contact, &c.CreatedAt); err != nil {
			a.log("error", "feedback_list_scan_failed", map[string]any{"error": err.Error()})
			http.Error(w, "failed to load comments", http.StatusInternalServerError)
			return
		}
		comments = append(comments, c)
	}
	if err := rows.Err(); err != nil {
		http.Error(w, "failed to load comments", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, feedbackListResponse{Limit: limit, Comments: comments})
}
