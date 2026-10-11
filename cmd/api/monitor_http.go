package main

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/projecthub/projecthub/internal/events"
	"github.com/projecthub/projecthub/internal/monitor"
	"github.com/projecthub/projecthub/internal/rbac"
)

type monitorHTTP struct {
	auth    *authHTTP
	service *monitor.Service
	broker  *events.Broker
}
type healthCheckPayload struct {
	ProjectID         string       `json:"project_id"`
	Kind              monitor.Kind `json:"kind"`
	Target            string       `json:"target"`
	IntervalSeconds   int          `json:"interval_seconds"`
	TimeoutSeconds    int          `json:"timeout_seconds"`
	FailureThreshold  int          `json:"failure_threshold"`
	RecoveryThreshold int          `json:"recovery_threshold"`
	Enabled           *bool        `json:"enabled"`
}

func (m *monitorHTTP) register(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/events", m.auth.require(rbac.PermissionRead, http.HandlerFunc(m.events)))
	mux.Handle("GET /api/v1/health-checks", m.auth.require(rbac.PermissionRead, http.HandlerFunc(m.list)))
	mux.Handle("POST /api/v1/health-checks", m.auth.require(rbac.PermissionManage, http.HandlerFunc(m.create)))
	mux.Handle("GET /api/v1/health-checks/{id}/samples", m.auth.require(rbac.PermissionRead, http.HandlerFunc(m.samples)))
	mux.Handle("DELETE /api/v1/health-checks/{id}", m.auth.require(rbac.PermissionManage, http.HandlerFunc(m.delete)))
}
func (m *monitorHTTP) list(w http.ResponseWriter, r *http.Request) {
	items, err := m.service.List(r.Context(), r.URL.Query().Get("project_id"))
	if err != nil {
		monitorError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items)})
}
func (m *monitorHTTP) create(w http.ResponseWriter, r *http.Request) {
	var body healthCheckPayload
	if decodeJSON(w, r, &body) != nil {
		badRequest(w)
		return
	}
	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	item, err := m.service.Create(r.Context(), body.ProjectID, monitor.CreateInput{Kind: body.Kind, Target: body.Target, IntervalSeconds: body.IntervalSeconds, TimeoutSeconds: body.TimeoutSeconds, FailureThreshold: body.FailureThreshold, RecoveryThreshold: body.RecoveryThreshold, Enabled: enabled})
	if err != nil {
		monitorError(w, err)
		return
	}
	if err = m.auth.auditTarget(r, principalFromRequest(r).User.ID, "health_check", item.ID, "health_check.create", "success"); err != nil {
		http.Error(w, "audit unavailable", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}
func (m *monitorHTTP) samples(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := m.service.Get(r.Context(), id); err != nil {
		monitorError(w, err)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := m.service.Samples(r.Context(), id, limit)
	if err != nil {
		monitorError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items)})
}
func (m *monitorHTTP) delete(w http.ResponseWriter, r *http.Request) {
	if err := m.service.Delete(r.Context(), r.PathValue("id")); err != nil {
		monitorError(w, err)
		return
	}
	if err := m.auth.auditTarget(r, principalFromRequest(r).User.ID, "health_check", r.PathValue("id"), "health_check.delete", "success"); err != nil {
		http.Error(w, "audit unavailable", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (m *monitorHTTP) events(w http.ResponseWriter, r *http.Request) {
	last := r.Header.Get("Last-Event-ID")
	after := uint64(0)
	if last != "" {
		value, err := strconv.ParseUint(last, 10, 64)
		if err != nil {
			http.Error(w, "invalid Last-Event-ID", http.StatusBadRequest)
			return
		}
		after = value
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	replay, sub := m.broker.Subscribe(after)
	defer sub.Close()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	for _, event := range replay {
		if !writeSSE(w, event) {
			return
		}
	}
	flusher.Flush()
	keepAlive := time.NewTicker(15 * time.Second)
	defer keepAlive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case event, open := <-sub.Events:
			if !open {
				return
			}
			if !writeSSE(w, event) {
				return
			}
			flusher.Flush()
		case <-keepAlive.C:
			if _, err := fmt.Fprint(w, ": keep-alive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
func writeSSE(w http.ResponseWriter, event events.Event) bool {
	if _, err := fmt.Fprintf(w, "id: %d\nevent: %s\n", event.ID, event.Type); err != nil {
		return false
	}
	for _, line := range strings.Split(string(event.Data), "\n") {
		if _, err := fmt.Fprintf(w, "data: %s\n", line); err != nil {
			return false
		}
	}
	_, err := fmt.Fprint(w, "\n")
	return err == nil
}
func monitorError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, monitor.ErrInvalidCheck):
		http.Error(w, "invalid health check", http.StatusBadRequest)
	case errors.Is(err, monitor.ErrProjectNotFound), errors.Is(err, monitor.ErrCheckNotFound):
		http.Error(w, "resource not found", http.StatusNotFound)
	default:
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}
