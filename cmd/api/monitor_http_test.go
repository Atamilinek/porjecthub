package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/projecthub/projecthub/internal/auth"
	"github.com/projecthub/projecthub/internal/events"
	"github.com/projecthub/projecthub/internal/monitor"
	"github.com/projecthub/projecthub/internal/projects"
)

func TestHealthCheckAPIRequiresAdminAndPersistsCRUD(t *testing.T) {
	db := testDatabase(t)
	authService := auth.NewService(db)
	admin, err := authService.BootstrapAdmin(t.Context(), "monitor-admin", "correct-horse-battery-staple")
	if err != nil {
		t.Fatal(err)
	}
	session, err := authService.NewSession(t.Context(), admin.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	project, err := projects.NewService(db).Create(t.Context(), projects.CreateInput{Name: "Monitored API project"})
	if err != nil {
		t.Fatal(err)
	}
	handler := newHandler(db)
	anon := httptest.NewRecorder()
	handler.ServeHTTP(anon, httptest.NewRequest(http.MethodGet, "/api/v1/health-checks", nil))
	if anon.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous health check list status=%d", anon.Code)
	}
	body, _ := json.Marshal(map[string]any{"project_id": project.ID, "kind": "http", "target": "https://status.example.com/health", "enabled": true})
	create := healthRequest(http.MethodPost, "/api/v1/health-checks", string(body), session)
	created := httptest.NewRecorder()
	handler.ServeHTTP(created, create)
	if created.Code != http.StatusCreated {
		t.Fatalf("create health check status=%d body=%s", created.Code, created.Body.String())
	}
	var item struct {
		ID       string `json:"id"`
		Interval int    `json:"interval_seconds"`
		Timeout  int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &item); err != nil || item.ID == "" || item.Interval != 30 || item.Timeout != 5 {
		t.Fatalf("health check response=%+v err=%v", item, err)
	}
	list := httptest.NewRecorder()
	handler.ServeHTTP(list, healthRequest(http.MethodGet, "/api/v1/health-checks?project_id="+project.ID, "", session))
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), item.ID) {
		t.Fatalf("health check list status=%d body=%s", list.Code, list.Body.String())
	}
	samples := httptest.NewRecorder()
	handler.ServeHTTP(samples, healthRequest(http.MethodGet, "/api/v1/health-checks/"+item.ID+"/samples", "", session))
	if samples.Code != http.StatusOK || !strings.Contains(samples.Body.String(), "\"count\":0") {
		t.Fatalf("sample fallback status=%d body=%s", samples.Code, samples.Body.String())
	}
	remove := httptest.NewRecorder()
	handler.ServeHTTP(remove, healthRequest(http.MethodDelete, "/api/v1/health-checks/"+item.ID, "", session))
	if remove.Code != http.StatusNoContent {
		t.Fatalf("delete health check status=%d body=%s", remove.Code, remove.Body.String())
	}
	var audits int
	if err := db.QueryRow("SELECT count(*) FROM audit_logs WHERE actor_id=? AND event_type='security' AND action='health_check.create'", admin.ID).Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("health check audit count=%d err=%v", audits, err)
	}
	var targetType, targetID string
	if err := db.QueryRow("SELECT target_type,target_id FROM audit_logs WHERE actor_id=? AND action='health_check.create'", admin.ID).Scan(&targetType, &targetID); err != nil || targetType != "health_check" || targetID != item.ID {
		t.Fatalf("health check audit target=(%q,%q) err=%v", targetType, targetID, err)
	}
	viewerID := "11111111-1111-4111-8111-111111111111"
	if _, err := db.Exec("INSERT INTO users(id,username,password_hash,role,disabled,created_at,updated_at) VALUES(?,?,?,'viewer',0,1,1)", viewerID, "monitor-viewer", "unused"); err != nil {
		t.Fatal(err)
	}
	viewerSession, err := authService.NewSession(t.Context(), viewerID, "")
	if err != nil {
		t.Fatal(err)
	}
	denied := httptest.NewRecorder()
	handler.ServeHTTP(denied, healthRequest(http.MethodPost, "/api/v1/health-checks", string(body), viewerSession))
	if denied.Code != http.StatusForbidden {
		t.Fatalf("viewer create health check status=%d body=%s", denied.Code, denied.Body.String())
	}
}

func TestSSERequiresSessionSupportsReplayAndRejectsBadCursor(t *testing.T) {
	db := testDatabase(t)
	service := auth.NewService(db)
	admin, err := service.BootstrapAdmin(t.Context(), "events-admin", "correct-horse-battery-staple")
	if err != nil {
		t.Fatal(err)
	}
	session, err := service.NewSession(t.Context(), admin.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	authRoutes := &authHTTP{service: service, limiter: auth.NewLoginLimiter(5, time.Minute), sourceLimiter: auth.NewLoginLimiter(20, time.Minute), db: db}
	broker := events.NewBroker(8, 4)
	data, _ := json.Marshal(map[string]string{"state": "healthy"})
	broker.Publish("health.state", data)
	mux := http.NewServeMux()
	authRoutes.register(mux)
	(&monitorHTTP{auth: authRoutes, service: monitor.NewService(db, nil, 1), broker: broker}).register(mux)
	handler := requestID(mux)
	anonymous := httptest.NewRecorder()
	handler.ServeHTTP(anonymous, httptest.NewRequest(http.MethodGet, "/api/v1/events", nil))
	if anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous SSE status=%d", anonymous.Code)
	}
	invalid := healthRequest(http.MethodGet, "/api/v1/events", "", session)
	invalid.Header.Set("Last-Event-ID", "not-a-number")
	bad := httptest.NewRecorder()
	handler.ServeHTTP(bad, invalid)
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("bad cursor status=%d", bad.Code)
	}
	ctx, cancel := context.WithCancel(t.Context())
	stopCancel := time.AfterFunc(20*time.Millisecond, cancel)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil).WithContext(ctx)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session.Token})
	req.Header.Set("Last-Event-ID", "0")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	stopCancel.Stop()
	cancel()
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "text/event-stream" || !strings.Contains(response.Body.String(), "id: 1\n") || !strings.Contains(response.Body.String(), "\"state\":\"healthy\"") {
		t.Fatalf("SSE response status=%d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
	}
}

func healthRequest(method, path, body string, session auth.Session) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Host = "localhost:8080"
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session.Token})
	if method != "GET" {
		req.Header.Set("Origin", "http://localhost:8080")
		req.Header.Set("X-CSRF-Token", session.CSRFToken)
	}
	return req
}
