package main

import (
	"database/sql"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/projecthub/projecthub/internal/agent"
	"github.com/projecthub/projecthub/internal/ids"
	"github.com/projecthub/projecthub/internal/jobs"
	"github.com/projecthub/projecthub/internal/rbac"
)

type agentHTTP struct {
	auth *authHTTP
	db   *sql.DB
	jobs *jobs.Dispatcher
}
type hostPayload struct {
	Name    string `json:"name"`
	AgentID string `json:"agent_id"`
}
type targetPayload struct {
	ProjectID     string     `json:"project_id"`
	HostID        string     `json:"host_id"`
	AgentTargetID string     `json:"agent_target_id"`
	Kind          agent.Kind `json:"kind"`
	DisplayName   string     `json:"display_name"`
}
type targetView struct {
	ID            string     `json:"id"`
	ProjectID     string     `json:"project_id"`
	HostID        string     `json:"host_id"`
	HostName      string     `json:"host_name"`
	AgentID       string     `json:"agent_id"`
	AgentTargetID string     `json:"agent_target_id"`
	Kind          agent.Kind `json:"kind"`
	DisplayName   string     `json:"display_name"`
	CreatedAt     int64      `json:"created_at"`
}

var agentIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:-]{0,127}$`)

func (a *agentHTTP) register(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/hosts", a.auth.require(rbac.PermissionRead, http.HandlerFunc(a.listHosts)))
	mux.Handle("POST /api/v1/hosts", a.auth.require(rbac.PermissionManage, http.HandlerFunc(a.createHost)))
	mux.Handle("DELETE /api/v1/hosts/{id}", a.auth.require(rbac.PermissionManage, http.HandlerFunc(a.deleteHost)))
	mux.Handle("GET /api/v1/service-targets", a.auth.require(rbac.PermissionRead, http.HandlerFunc(a.listTargets)))
	mux.Handle("POST /api/v1/service-targets", a.auth.require(rbac.PermissionManage, http.HandlerFunc(a.createTarget)))
	mux.Handle("DELETE /api/v1/service-targets/{id}", a.auth.require(rbac.PermissionManage, http.HandlerFunc(a.deleteTarget)))
	mux.Handle("GET /api/v1/service-targets/{id}/status", a.auth.require(rbac.PermissionRead, http.HandlerFunc(a.targetStatus)))
	mux.Handle("POST /api/v1/service-targets/{id}/actions", a.auth.require(rbac.PermissionManage, http.HandlerFunc(a.createAction)))
	mux.Handle("GET /api/v1/action-jobs", a.auth.require(rbac.PermissionRead, http.HandlerFunc(a.listJobs)))
	mux.Handle("GET /api/v1/action-jobs/{id}", a.auth.require(rbac.PermissionRead, http.HandlerFunc(a.getJob)))
}
func (a *agentHTTP) targetStatus(w http.ResponseWriter, r *http.Request) {
	result, err := a.jobs.Status(r.Context(), r.PathValue("id"))
	if err != nil {
		switch {
		case errors.Is(err, jobs.ErrTargetNotFound):
			http.NotFound(w, r)
		case errors.Is(err, agent.ErrAgentUnavailable):
			http.Error(w, "agent unavailable", 503)
		default:
			var remote *agent.RemoteError
			if errors.As(err, &remote) {
				http.Error(w, "agent status unavailable", remote.Status)
			} else {
				http.Error(w, "agent status unavailable", 502)
			}
		}
		return
	}
	writeJSON(w, 200, result)
}
func (a *agentHTTP) listHosts(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.QueryContext(r.Context(), "SELECT id,name,agent_id,last_seen_at,created_at,updated_at FROM hosts ORDER BY name")
	if err != nil {
		http.Error(w, "internal error", 500)
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, name, agentID string
		var seen sql.NullInt64
		var created, updated int64
		if err := rows.Scan(&id, &name, &agentID, &seen, &created, &updated); err != nil {
			http.Error(w, "internal error", 500)
			return
		}
		var last any
		if seen.Valid {
			last = seen.Int64
		}
		items = append(items, map[string]any{"id": id, "name": name, "agent_id": agentID, "last_seen_at": last, "created_at": created, "updated_at": updated})
	}
	if err := rows.Err(); err != nil {
		http.Error(w, "internal error", 500)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items, "count": len(items)})
}
func (a *agentHTTP) createHost(w http.ResponseWriter, r *http.Request) {
	var body hostPayload
	if decodeJSON(w, r, &body) != nil {
		badRequest(w)
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	body.AgentID = strings.TrimSpace(body.AgentID)
	if len(body.Name) < 2 || len(body.Name) > 100 || strings.ContainsAny(body.Name, "\r\n\x00") || !agentIDPattern.MatchString(body.AgentID) {
		http.Error(w, "invalid host", 400)
		return
	}
	id, err := ids.NewUUID()
	if err != nil {
		http.Error(w, "id unavailable", 500)
		return
	}
	now := time.Now().Unix()
	if _, err := a.db.ExecContext(r.Context(), "INSERT INTO hosts(id,name,agent_id,created_at,updated_at) VALUES(?,?,?,?,?)", id, body.Name, body.AgentID, now, now); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			http.Error(w, "host already exists", 409)
		} else {
			http.Error(w, "host unavailable", 500)
		}
		return
	}
	if err := a.auth.auditTarget(r, principalFromRequest(r).User.ID, "host", id, "host.create", "success"); err != nil {
		http.Error(w, "audit unavailable", 500)
		return
	}
	writeJSON(w, 201, map[string]any{"id": id, "name": body.Name, "agent_id": body.AgentID, "last_seen_at": nil, "created_at": now, "updated_at": now})
}
func (a *agentHTTP) deleteHost(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	result, err := a.db.ExecContext(r.Context(), "DELETE FROM hosts WHERE id=?", id)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "foreign key") {
			http.Error(w, "host has service targets", 409)
		} else {
			http.Error(w, "host unavailable", 500)
		}
		return
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		http.NotFound(w, r)
		return
	}
	if err := a.auth.auditTarget(r, principalFromRequest(r).User.ID, "host", id, "host.delete", "success"); err != nil {
		http.Error(w, "audit unavailable", 500)
		return
	}
	w.WriteHeader(204)
}
func (a *agentHTTP) listTargets(w http.ResponseWriter, r *http.Request) {
	query := `SELECT t.id,t.project_id,t.host_id,h.name,h.agent_id,t.agent_target_id,t.kind,t.display_name,t.created_at FROM service_targets t JOIN hosts h ON h.id=t.host_id`
	args := []any{}
	if projectID := r.URL.Query().Get("project_id"); projectID != "" {
		if !ids.IsUUID(projectID) {
			badRequest(w)
			return
		}
		query += " WHERE t.project_id=?"
		args = append(args, projectID)
	}
	query += " ORDER BY t.display_name,t.id"
	rows, err := a.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		http.Error(w, "internal error", 500)
		return
	}
	defer rows.Close()
	items := make([]targetView, 0)
	for rows.Next() {
		var item targetView
		if err := rows.Scan(&item.ID, &item.ProjectID, &item.HostID, &item.HostName, &item.AgentID, &item.AgentTargetID, &item.Kind, &item.DisplayName, &item.CreatedAt); err != nil {
			http.Error(w, "internal error", 500)
			return
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		http.Error(w, "internal error", 500)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items, "count": len(items)})
}
func (a *agentHTTP) createTarget(w http.ResponseWriter, r *http.Request) {
	var body targetPayload
	if decodeJSON(w, r, &body) != nil {
		badRequest(w)
		return
	}
	body.DisplayName = strings.TrimSpace(body.DisplayName)
	body.AgentTargetID = strings.TrimSpace(body.AgentTargetID)
	if !ids.IsUUID(body.ProjectID) || !ids.IsUUID(body.HostID) || !agentIDPattern.MatchString(body.AgentTargetID) || len(body.DisplayName) < 1 || len(body.DisplayName) > 120 || (body.Kind != agent.KindDocker && body.Kind != agent.KindSystemd) {
		http.Error(w, "invalid target mapping", 400)
		return
	}
	id, err := ids.NewUUID()
	if err != nil {
		http.Error(w, "id unavailable", 500)
		return
	}
	now := time.Now().Unix()
	if _, err := a.db.ExecContext(r.Context(), "INSERT INTO service_targets(id,project_id,host_id,agent_target_id,kind,display_name,created_at) VALUES(?,?,?,?,?,?,?)", id, body.ProjectID, body.HostID, body.AgentTargetID, body.Kind, body.DisplayName, now); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			http.Error(w, "target mapping already exists", 409)
		} else {
			http.Error(w, "project or host not found", 404)
		}
		return
	}
	if err := a.auth.auditTarget(r, principalFromRequest(r).User.ID, "service_target", id, "service_target.create", "success"); err != nil {
		http.Error(w, "audit unavailable", 500)
		return
	}
	writeJSON(w, 201, map[string]any{"id": id, "project_id": body.ProjectID, "host_id": body.HostID, "agent_target_id": body.AgentTargetID, "kind": body.Kind, "display_name": body.DisplayName, "created_at": now})
}
func (a *agentHTTP) deleteTarget(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var checks int
	if err := a.db.QueryRowContext(r.Context(), "SELECT count(*) FROM health_checks WHERE kind='agent' AND target=?", id).Scan(&checks); err != nil {
		http.Error(w, "target unavailable", 500)
		return
	}
	if checks > 0 {
		http.Error(w, "target has Agent health checks", 409)
		return
	}
	res, err := a.db.ExecContext(r.Context(), "DELETE FROM service_targets WHERE id=?", id)
	if err != nil {
		http.Error(w, "target has action history", 409)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		http.NotFound(w, r)
		return
	}
	if err := a.auth.auditTarget(r, principalFromRequest(r).User.ID, "service_target", id, "service_target.delete", "success"); err != nil {
		http.Error(w, "audit unavailable", 500)
		return
	}
	w.WriteHeader(204)
}
func (a *agentHTTP) createAction(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action agent.Action `json:"action"`
	}
	if decodeJSON(w, r, &body) != nil {
		badRequest(w)
		return
	}
	key := r.Header.Get("Idempotency-Key")
	job, fresh, err := a.jobs.Create(r.Context(), jobs.CreateInput{ActorID: principalFromRequest(r).User.ID, TargetID: r.PathValue("id"), Action: body.Action, IdempotencyKey: key})
	if err != nil {
		switch {
		case errors.Is(err, jobs.ErrTargetNotFound):
			http.NotFound(w, r)
		case errors.Is(err, jobs.ErrIdempotencyConflict):
			http.Error(w, "idempotency key conflict", 409)
		case errors.Is(err, jobs.ErrQueueFull):
			writeJSON(w, 503, map[string]any{"error": "action queue full", "job": job})
		case errors.Is(err, jobs.ErrInvalidAction):
			http.Error(w, "invalid action or idempotency key", 400)
		case errors.Is(err, agent.ErrAgentUnavailable):
			http.Error(w, "agent unavailable", 503)
		default:
			http.Error(w, "action unavailable", 500)
		}
		return
	}
	status := http.StatusAccepted
	if !fresh {
		status = http.StatusOK
	}
	writeJSON(w, status, job)
}
func (a *agentHTTP) listJobs(w http.ResponseWriter, r *http.Request) {
	items, err := a.jobs.List(r.Context(), 100)
	if err != nil {
		http.Error(w, "jobs unavailable", 500)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items, "count": len(items)})
}
func (a *agentHTTP) getJob(w http.ResponseWriter, r *http.Request) {
	item, err := a.jobs.Get(r.Context(), r.PathValue("id"))
	if errors.Is(err, jobs.ErrJobNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "job unavailable", 500)
		return
	}
	writeJSON(w, 200, item)
}
