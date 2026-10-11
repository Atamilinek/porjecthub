package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/projecthub/projecthub/internal/auth"
	"github.com/projecthub/projecthub/internal/rbac"
)

func TestAdminAuditUsersAndSettingsAreProtectedAndFunctional(t *testing.T) {
	db := testDatabase(t)
	service := auth.NewService(db)
	admin, err := service.BootstrapAdmin(t.Context(), "audit-admin", "correct-horse-battery-staple")
	if err != nil {
		t.Fatal(err)
	}
	adminSession, err := service.NewSession(t.Context(), admin.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	viewer, err := service.CreateUser(t.Context(), "read-only", "another-correct-horse", rbac.RoleViewer)
	if err != nil {
		t.Fatal(err)
	}
	viewerSession, err := service.NewSession(t.Context(), viewer.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	handler, _, _ := newRuntime(db, nil, nil)
	for _, path := range []string{"/api/v1/admin/audit", "/api/v1/admin/users", "/api/v1/admin/settings"} {
		anonymous := httptest.NewRecorder()
		handler.ServeHTTP(anonymous, httptest.NewRequest(http.MethodGet, path, nil))
		if anonymous.Code != http.StatusUnauthorized {
			t.Fatalf("anonymous %s status=%d", path, anonymous.Code)
		}
		denied := httptest.NewRecorder()
		handler.ServeHTTP(denied, healthRequest(http.MethodGet, path, "", viewerSession))
		if denied.Code != http.StatusForbidden {
			t.Fatalf("viewer %s status=%d body=%s", path, denied.Code, denied.Body.String())
		}
	}
	created := httptest.NewRecorder()
	handler.ServeHTTP(created, healthRequest(http.MethodPost, "/api/v1/admin/users", `{"username":"operator.one","password":"operator-password-long","role":"operator"}`, adminSession))
	if created.Code != http.StatusCreated || strings.Contains(created.Body.String(), "password_hash") {
		t.Fatalf("create user status=%d body=%s", created.Code, created.Body.String())
	}
	var user auth.User
	if err = json.Unmarshal(created.Body.Bytes(), &user); err != nil || user.Role != rbac.RoleOperator {
		t.Fatalf("user=%+v err=%v", user, err)
	}
	list := httptest.NewRecorder()
	handler.ServeHTTP(list, healthRequest(http.MethodGet, "/api/v1/admin/users", "", adminSession))
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), "operator.one") {
		t.Fatalf("users status=%d body=%s", list.Code, list.Body.String())
	}
	disable := httptest.NewRecorder()
	handler.ServeHTTP(disable, healthRequest(http.MethodPatch, "/api/v1/admin/users/"+user.ID, `{"disabled":true}`, adminSession))
	if disable.Code != http.StatusNoContent {
		t.Fatalf("disable status=%d body=%s", disable.Code, disable.Body.String())
	}
	if _, err = service.Authenticate(t.Context(), "operator.one", "operator-password-long"); err == nil {
		t.Fatal("disabled user authenticated")
	}
	audit := httptest.NewRecorder()
	handler.ServeHTTP(audit, healthRequest(http.MethodGet, "/api/v1/admin/audit?q=user.", "", adminSession))
	if audit.Code != http.StatusOK || !strings.Contains(audit.Body.String(), "user.create") || !strings.Contains(audit.Body.String(), "user.disable") {
		t.Fatalf("audit status=%d body=%s", audit.Code, audit.Body.String())
	}
	settings := httptest.NewRecorder()
	handler.ServeHTTP(settings, healthRequest(http.MethodGet, "/api/v1/admin/settings", "", adminSession))
	if settings.Code != http.StatusOK || !strings.Contains(settings.Body.String(), `"agent_transport_configured":false`) || !strings.Contains(settings.Body.String(), `"sample_retention_days":30`) {
		t.Fatalf("settings status=%d body=%s", settings.Code, settings.Body.String())
	}
}
