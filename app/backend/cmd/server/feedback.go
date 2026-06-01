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
	feedbackDefaultLimit    = 50
	feedbackMaxLimit        = 200
	feedbackActorNameMaxLen = 200
)

type feedbackCreateRequest struct {
	Message string `json:"message"`
}

type feedbackComment struct {
	ID        int64     `json:"id"`
	Message   string    `json:"message"`
	ActorKind string    `json:"actor_kind"`
	ActorID   string    `json:"actor_id,omitempty"`
	ActorRole string    `json:"actor_role,omitempty"`
	ActorName string    `json:"actor_name,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type feedbackListResponse struct {
	Limit    int               `json:"limit"`
	Comments []feedbackComment `json:"comments"`
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
		INSERT INTO feedback_comments (message, actor_kind, actor_id, actor_role, actor_name, client_ip)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id
	`,
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

	rows, err := a.db.QueryContext(ctx, `
		SELECT id,
		       message,
		       actor_kind,
		       COALESCE(actor_id, '')   AS actor_id,
		       COALESCE(actor_role, '') AS actor_role,
		       COALESCE(actor_name, '') AS actor_name,
		       created_at
		FROM feedback_comments
		ORDER BY created_at DESC, id DESC
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
		if err := rows.Scan(&c.ID, &c.Message, &c.ActorKind, &c.ActorID, &c.ActorRole, &c.ActorName, &c.CreatedAt); err != nil {
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
