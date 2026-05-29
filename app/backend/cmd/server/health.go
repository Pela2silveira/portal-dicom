package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type ComponentSeverity string

const (
	ComponentSeverityRequired ComponentSeverity = "required"
	ComponentSeverityOptional ComponentSeverity = "optional"
)

type ComponentStatus string

const (
	ComponentStatusHealthy     ComponentStatus = "healthy"
	ComponentStatusUnavailable ComponentStatus = "unavailable"
	ComponentStatusUnknown     ComponentStatus = "unknown"
)

type ComponentHealth struct {
	Name        string            `json:"name"`
	DisplayName string            `json:"display_name,omitempty"`
	Category    string            `json:"category"`
	Severity    ComponentSeverity `json:"severity"`
	Status      ComponentStatus   `json:"status"`
	Message     string            `json:"message,omitempty"`
}

type SystemHealthEvent struct {
	Status     string            `json:"status"`
	Components []ComponentHealth `json:"components"`
	TS         string            `json:"ts"`
}

type PublicSystemHealthEvent struct {
	Status string `json:"status"`
	TS     string `json:"ts"`
}

type dependencyHealthReporter interface {
	Healthy() bool
}

type HealthResponse struct {
	Status              string            `json:"status"`
	AppEnv              string            `json:"app_env"`
	DBOK                bool              `json:"db_ok"`
	OrthancOK           bool              `json:"orthanc_ok"`
	ConfigOK            bool              `json:"config_ok"`
	IdentityProvidersOK bool              `json:"identity_providers_ok"`
	CheckedAt           string            `json:"checked_at"`
	ConfigPath          string            `json:"config_path"`
	Components          []ComponentHealth `json:"components"`
}

type PublicHealthResponse struct {
	Status    string `json:"status"`
	CheckedAt string `json:"checked_at"`
}

type PACSNodeHealthConfig struct {
	Mode       string `json:"mode"`
	CallingAET string `json:"calling_aet"`
}

type PACSNodeHealthResponse struct {
	Mode       string `json:"mode"`
	CallingAET string `json:"calling_aet,omitempty"`
}

type HealthAdapter interface {
	Check(ctx context.Context, node PACSNodeResolvedConfig) error
}
type HTTPHealthAdapter struct{}
type MixedHealthAdapter struct{}

const (
	systemHealthCheckTimeout = 8 * time.Second
	dimseEchoHealthTimeout   = 7 * time.Second
	orthancCFindTimeout      = 75 * time.Second
	retrieveJobStaleAfter    = 10 * time.Minute
)

func (a *App) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	event := a.currentSystemHealthEvent()
	components := event.Components
	checkedAt := event.TS
	if checkedAt == "" {
		checkedAt = time.Now().UTC().Format(time.RFC3339)
	}

	resp := HealthResponse{
		Status:              event.Status,
		AppEnv:              a.cfg.AppEnv,
		DBOK:                componentHealthy(components, "postgres"),
		OrthancOK:           componentHealthy(components, "orthanc"),
		ConfigOK:            componentHealthy(components, "config"),
		IdentityProvidersOK: componentHealthy(components, "mongo_identity"),
		CheckedAt:           checkedAt,
		ConfigPath:          a.cfg.ConfigPath,
		Components:          components,
	}

	statusCode := http.StatusOK
	if resp.Status == "unavailable" {
		statusCode = http.StatusServiceUnavailable
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if r.Header.Get("X-Portal-Internal-Health") == "1" {
		_ = json.NewEncoder(w).Encode(resp)
	} else {
		_ = json.NewEncoder(w).Encode(PublicHealthResponse{
			Status:    resp.Status,
			CheckedAt: resp.CheckedAt,
		})
	}

	a.log("info", "health_checked", map[string]any{
		"status":                resp.Status,
		"db_ok":                 resp.DBOK,
		"orthanc_ok":            resp.OrthancOK,
		"config_ok":             resp.ConfigOK,
		"identity_providers_ok": resp.IdentityProvidersOK,
		"status_code":           statusCode,
	})
}

func (a *App) handleSystemEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	initialEvent := a.currentSystemHealthEvent()
	if err := writeSystemHealthSSEEvent(w, "health_status_changed", publicSystemHealthEvent(initialEvent)); err != nil {
		return
	}
	flusher.Flush()

	subscriber := a.subscribeSystemHealth()
	defer a.unsubscribeSystemHealth(subscriber)

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case event := <-subscriber:
			if err := writeSystemHealthSSEEvent(w, "health_status_changed", publicSystemHealthEvent(event)); err != nil {
				return
			}
			flusher.Flush()
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": keep-alive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (a *App) collectComponentHealth(ctx context.Context) []ComponentHealth {
	components := []ComponentHealth{
		{
			Name:     "backend",
			Category: "core_required",
			Severity: ComponentSeverityRequired,
			Status:   ComponentStatusHealthy,
			Message:  "process alive",
		},
		{
			Name:     "config",
			Category: "core_required",
			Severity: ComponentSeverityRequired,
			Status:   boolToComponentStatus(a.checkConfig()),
		},
		{
			Name:     "postgres",
			Category: "core_required",
			Severity: ComponentSeverityRequired,
			Status:   boolToComponentStatus(a.checkDB(ctx)),
		},
		{
			Name:     "orthanc",
			Category: "core_required",
			Severity: ComponentSeverityRequired,
			Status:   boolToComponentStatus(a.checkOrthanc(ctx)),
		},
	}

	if mongoComponent, ok := a.mongoIdentityComponent(); ok {
		components = append(components, mongoComponent)
	}

	components = append(components, a.remotePACSComponents(ctx)...)

	return components
}

func (a *App) remotePACSComponents(ctx context.Context) []ComponentHealth {
	if a.externalConfig == nil {
		return nil
	}

	components := make([]ComponentHealth, 0, len(a.externalConfig.PACSNodes))
	for _, node := range a.externalConfig.PACSNodes {
		resolved := node.Resolved()
		status := boolToComponentStatus(a.checkRemotePACS(ctx, node))
		message := "remote pacs reachable"
		switch resolved.HealthMode {
		case "dimse_c_echo":
			message = "dimse echo reachable"
			if status != ComponentStatusHealthy {
				message = "dimse echo unreachable"
			}
		case "auth_qido":
			message = "dicomweb reachable"
			if status != ComponentStatusHealthy {
				message = "dicomweb unreachable"
			}
		case "http", "mixed", "":
			message = "dicomweb reachable"
			if status != ComponentStatusHealthy {
				message = "dicomweb unreachable"
			}
		default:
			message = "health check result"
		}

		components = append(components, ComponentHealth{
			Name:        "remote_pacs:" + node.ID,
			DisplayName: strings.TrimSpace(resolved.Name),
			Category:    "optional",
			Severity:    ComponentSeverityOptional,
			Status:      status,
			Message:     message,
		})
	}

	return components
}

func componentHealthy(components []ComponentHealth, name string) bool {
	for _, component := range components {
		if component.Name == name {
			return component.Status == ComponentStatusHealthy
		}
	}
	return false
}

func overallHealthStatus(components []ComponentHealth) string {
	optionalDegraded := false
	for _, component := range components {
		if component.Status == ComponentStatusUnknown {
			continue
		}
		if component.Severity == ComponentSeverityRequired && component.Status != ComponentStatusHealthy {
			return "unavailable"
		}
		if component.Severity == ComponentSeverityOptional && component.Status != ComponentStatusHealthy {
			optionalDegraded = true
		}
	}
	if optionalDegraded {
		return "degraded"
	}
	return "ok"
}

func boolToComponentStatus(ok bool) ComponentStatus {
	if ok {
		return ComponentStatusHealthy
	}
	return ComponentStatusUnavailable
}

func (a *App) startSystemHealthWatcher() {
	a.updateSystemHealthState()

	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			a.updateSystemHealthState()
		}
	}()
}

func (a *App) updateSystemHealthState() {
	ctx, cancel := context.WithTimeout(context.Background(), systemHealthCheckTimeout)
	defer cancel()

	components := a.collectComponentHealth(ctx)
	event := SystemHealthEvent{
		Status:     overallHealthStatus(components),
		Components: components,
		TS:         time.Now().UTC().Format(time.RFC3339),
	}

	a.systemHealthStateMu.Lock()
	prev := a.systemHealthState
	a.systemHealthState = event
	a.systemHealthStateMu.Unlock()

	if systemHealthSignature(prev) == systemHealthSignature(event) {
		return
	}

	a.log("info", "system_health_state_changed", map[string]any{
		"previous_status": prev.Status,
		"status":          event.Status,
	})

	a.publishSystemHealth(event)
}

func (a *App) currentSystemHealthEvent() SystemHealthEvent {
	a.systemHealthStateMu.RLock()
	state := a.systemHealthState
	a.systemHealthStateMu.RUnlock()
	if state.Status == "" {
		return SystemHealthEvent{
			Status:     "unknown",
			Components: nil,
			TS:         time.Now().UTC().Format(time.RFC3339),
		}
	}
	return state
}

func systemHealthSignature(event SystemHealthEvent) string {
	type signature struct {
		Status     string            `json:"status"`
		Components []ComponentHealth `json:"components"`
	}
	payload, _ := json.Marshal(signature{
		Status:     event.Status,
		Components: event.Components,
	})
	return string(payload)
}

func publicSystemHealthEvent(event SystemHealthEvent) PublicSystemHealthEvent {
	return PublicSystemHealthEvent{
		Status: event.Status,
		TS:     event.TS,
	}
}

func (a *HTTPHealthAdapter) Check(_ context.Context, _ PACSNodeResolvedConfig) error {
	return errors.New("http health adapter not implemented")
}

func (a *MixedHealthAdapter) Check(_ context.Context, _ PACSNodeResolvedConfig) error {
	return errors.New("mixed health adapter not implemented")
}

func (a *App) subscribeSystemHealth() chan SystemHealthEvent {
	a.systemEventMu.Lock()
	defer a.systemEventMu.Unlock()

	ch := make(chan SystemHealthEvent, 4)
	a.systemEventSubscribers[ch] = struct{}{}
	return ch
}

func (a *App) unsubscribeSystemHealth(ch chan SystemHealthEvent) {
	a.systemEventMu.Lock()
	defer a.systemEventMu.Unlock()

	delete(a.systemEventSubscribers, ch)
	close(ch)
}

func (a *App) publishSystemHealth(event SystemHealthEvent) {
	a.systemEventMu.Lock()
	defer a.systemEventMu.Unlock()

	a.log("info", "system_health_event_published", map[string]any{
		"status":           event.Status,
		"subscriber_count": len(a.systemEventSubscribers),
	})

	for subscriber := range a.systemEventSubscribers {
		select {
		case subscriber <- event:
		default:
		}
	}
}

func writeSystemHealthSSEEvent(w io.Writer, eventName string, event any) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\n", eventName); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
		return err
	}
	return nil
}
