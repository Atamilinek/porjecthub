package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/projecthub/projecthub/internal/auth"
	"github.com/projecthub/projecthub/internal/rbac"
)

func TestViewerCannotReachManagementHandler(t *testing.T) {
	db := testDatabase(t)
	service := auth.NewService(db)
	viewerID := "d2719a4c-cf77-4c7d-8a8e-52163a58e579"
	if _, err := db.Exec("INSERT INTO users(id,username,password_hash,role,disabled,created_at,updated_at) VALUES(?,?,?,'viewer',0,1,1)", viewerID, "read-only", "unused"); err != nil {
		t.Fatal(err)
	}
	session, err := service.NewSession(t.Context(), viewerID, "")
	if err != nil {
		t.Fatal(err)
	}
	reached := false
	protected := (&authHTTP{service: service, limiter: auth.NewLoginLimiter(5, 0), db: db}).require(rbac.PermissionManage, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true; w.WriteHeader(http.StatusNoContent) }))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/settings", nil)
	req.Host = "localhost:8080"
	req.Header.Set("Origin", "http://localhost:8080")
	req.Header.Set("X-CSRF-Token", session.CSRFToken)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session.Token})
	response := httptest.NewRecorder()
	protected.ServeHTTP(response, req)
	if response.Code != http.StatusForbidden || reached {
		t.Fatalf("viewer management status=%d handlerReached=%v", response.Code, reached)
	}
}
