package main

import (
	"database/sql"
	"errors"
	"net/http"
	"net/netip"
	"strconv"
	"strings"

	"github.com/projecthub/projecthub/internal/auth"
	"github.com/projecthub/projecthub/internal/ids"
	"github.com/projecthub/projecthub/internal/rbac"
)

type adminSettings struct {
	AgentTransportConfigured bool     `json:"agent_transport_configured"`
	ProbeAllowCIDRs          []string `json:"probe_allow_cidrs"`
	DefaultProbeInterval     int      `json:"default_probe_interval_seconds"`
	DefaultProbeTimeout      int      `json:"default_probe_timeout_seconds"`
	SampleRetentionDays      int      `json:"sample_retention_days"`
	SessionLifetimeHours     int      `json:"session_lifetime_hours"`
	Storage                  string   `json:"storage"`
}

type adminHTTP struct {
	auth     *authHTTP
	users    *auth.Service
	db       *sql.DB
	settings adminSettings
}

func (a *adminHTTP) register(mux *http.ServeMux) {
	admin := func(method, path string, handler http.HandlerFunc) {
		mux.Handle(method+" /api/v1/admin/"+path, a.auth.require(rbac.PermissionManage, handler))
	}
	admin("GET", "settings", a.getSettings)
	admin("GET", "audit", a.listAudit)
	admin("GET", "users", a.listUsers)
	admin("POST", "users", a.createUser)
	admin("PATCH", "users/{id}", a.setUserDisabled)
	admin("PUT", "users/{id}/password", a.resetPassword)
}

func (a *adminHTTP) getSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.settings)
}

type auditItem struct {
	ID            string `json:"id"`
	ActorID       string `json:"actor_id"`
	ActorUsername string `json:"actor_username"`
	RequestID     string `json:"request_id"`
	EventType     string `json:"event_type"`
	TargetType    string `json:"target_type"`
	TargetID      string `json:"target_id"`
	Action        string `json:"action"`
	Outcome       string `json:"outcome"`
	Detail        string `json:"detail"`
	CreatedAt     int64  `json:"created_at"`
}

func (a *adminHTTP) listAudit(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	limit, offset := 50, 0
	var err error
	if raw := query.Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 100 {
			badRequest(w)
			return
		}
	}
	if raw := query.Get("offset"); raw != "" {
		offset, err = strconv.Atoi(raw)
		if err != nil || offset < 0 || offset > 1000000 {
			badRequest(w)
			return
		}
	}
	filter := strings.TrimSpace(query.Get("q"))
	if len(filter) > 100 {
		badRequest(w)
		return
	}
	rows, err := a.db.QueryContext(r.Context(), `SELECT l.id,COALESCE(l.actor_id,''),COALESCE(u.username,''),l.request_id,l.event_type,l.target_type,l.target_id,l.action,l.outcome,l.detail,l.created_at FROM audit_logs l LEFT JOIN users u ON u.id=l.actor_id WHERE (?='' OR l.action LIKE '%'||?||'%' OR l.event_type LIKE '%'||?||'%' OR l.outcome LIKE '%'||?||'%' OR COALESCE(u.username,'') LIKE '%'||?||'%') ORDER BY l.created_at DESC,l.id DESC LIMIT ? OFFSET ?`, filter, filter, filter, filter, filter, limit, offset)
	if err != nil {
		http.Error(w, "audit unavailable", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	items := make([]auditItem, 0, limit)
	for rows.Next() {
		var item auditItem
		if err = rows.Scan(&item.ID, &item.ActorID, &item.ActorUsername, &item.RequestID, &item.EventType, &item.TargetType, &item.TargetID, &item.Action, &item.Outcome, &item.Detail, &item.CreatedAt); err != nil {
			http.Error(w, "audit unavailable", 500)
			return
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		http.Error(w, "audit unavailable", 500)
		return
	}
	var count int
	if err = a.db.QueryRowContext(r.Context(), `SELECT count(*) FROM audit_logs l LEFT JOIN users u ON u.id=l.actor_id WHERE (?='' OR l.action LIKE '%'||?||'%' OR l.event_type LIKE '%'||?||'%' OR l.outcome LIKE '%'||?||'%' OR COALESCE(u.username,'') LIKE '%'||?||'%')`, filter, filter, filter, filter, filter).Scan(&count); err != nil {
		http.Error(w, "audit unavailable", 500)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "count": count, "limit": limit, "offset": offset})
}

func (a *adminHTTP) listUsers(w http.ResponseWriter, r *http.Request) {
	items, err := a.users.ListUsers(r.Context())
	if err != nil {
		http.Error(w, "users unavailable", 500)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (a *adminHTTP) createUser(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string    `json:"username"`
		Password string    `json:"password"`
		Role     rbac.Role `json:"role"`
	}
	if decodeJSON(w, r, &body) != nil {
		badRequest(w)
		return
	}
	item, err := a.users.CreateUser(r.Context(), body.Username, body.Password, body.Role)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidUsername), errors.Is(err, auth.ErrWeakPassword), errors.Is(err, auth.ErrInvalidRole):
			http.Error(w, err.Error(), http.StatusBadRequest)
		case strings.Contains(strings.ToLower(err.Error()), "unique"):
			http.Error(w, "username already exists", http.StatusConflict)
		default:
			http.Error(w, "user unavailable", http.StatusInternalServerError)
		}
		return
	}
	if err = a.auth.auditTarget(r, principalFromRequest(r).User.ID, "user", item.ID, "user.create", "success"); err != nil {
		http.Error(w, "audit unavailable", 500)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (a *adminHTTP) setUserDisabled(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !ids.IsUUID(id) {
		http.NotFound(w, r)
		return
	}
	var body struct {
		Disabled *bool `json:"disabled"`
	}
	if decodeJSON(w, r, &body) != nil || body.Disabled == nil {
		badRequest(w)
		return
	}
	if id == principalFromRequest(r).User.ID && *body.Disabled {
		http.Error(w, "cannot disable your own active account", http.StatusConflict)
		return
	}
	err := a.users.SetUserDisabled(r.Context(), id, *body.Disabled)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if errors.Is(err, auth.ErrLastAdministrator) {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	if err != nil {
		http.Error(w, "user update unavailable", 500)
		return
	}
	action, outcome := "user.enable", "success"
	if *body.Disabled {
		action = "user.disable"
	}
	if err = a.auth.auditTarget(r, principalFromRequest(r).User.ID, "user", id, action, outcome); err != nil {
		http.Error(w, "audit unavailable", 500)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *adminHTTP) resetPassword(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !ids.IsUUID(id) {
		http.NotFound(w, r)
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if decodeJSON(w, r, &body) != nil {
		badRequest(w)
		return
	}
	err := a.users.ResetUserPassword(r.Context(), id, body.Password)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if errors.Is(err, auth.ErrWeakPassword) {
		badRequest(w)
		return
	}
	if err != nil {
		http.Error(w, "password reset unavailable", 500)
		return
	}
	if err = a.auth.auditTarget(r, principalFromRequest(r).User.ID, "user", id, "user.password_reset", "success"); err != nil {
		http.Error(w, "audit unavailable", 500)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func runtimeSettings(probeCIDRs []netip.Prefix, agentConfigured bool) adminSettings {
	allowed := make([]string, 0, len(probeCIDRs))
	for _, prefix := range probeCIDRs {
		allowed = append(allowed, prefix.String())
	}
	return adminSettings{AgentTransportConfigured: agentConfigured, ProbeAllowCIDRs: allowed, DefaultProbeInterval: 30, DefaultProbeTimeout: 5, SampleRetentionDays: 30, SessionLifetimeHours: 12, Storage: "SQLite WAL"}
}
