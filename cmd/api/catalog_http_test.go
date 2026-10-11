package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/projecthub/projecthub/internal/auth"
)

func TestAuthenticatedCatalogCRUDAndFilters(t *testing.T) {
	db := testDatabase(t)
	service := auth.NewService(db)
	admin, err := service.BootstrapAdmin(t.Context(), "catalog-admin", "correct-horse-battery-staple")
	if err != nil {
		t.Fatal(err)
	}
	session, err := service.NewSession(t.Context(), admin.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	handler := newHandler(db)
	categoryReq := catalogRequest(http.MethodPost, "/api/v1/categories", `{"name":"Engineering","icon":"🛠️","sort_order":1}`, session)
	categoryRes := httptest.NewRecorder()
	handler.ServeHTTP(categoryRes, categoryReq)
	if categoryRes.Code != http.StatusCreated {
		t.Fatalf("create category status=%d body=%s", categoryRes.Code, categoryRes.Body.String())
	}
	var category struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(categoryRes.Body.Bytes(), &category); err != nil || category.ID == "" {
		t.Fatalf("category response missing ID: %s", categoryRes.Body.String())
	}
	categoryUpdate := catalogRequest(http.MethodPut, "/api/v1/categories/"+category.ID, `{"name":"Platform","icon":"","sort_order":2}`, session)
	categoryUpdateRes := httptest.NewRecorder()
	handler.ServeHTTP(categoryUpdateRes, categoryUpdate)
	if categoryUpdateRes.Code != http.StatusOK || !strings.Contains(categoryUpdateRes.Body.String(), "Platform") {
		t.Fatalf("update category status=%d body=%s", categoryUpdateRes.Code, categoryUpdateRes.Body.String())
	}
	projectBody := `{"name":"Compiler","description":"Build service","category_id":"` + category.ID + `","links":[{"label":"Repository","url":"https://example.com/project"}]}`
	projectReq := catalogRequest(http.MethodPost, "/api/v1/projects", projectBody, session)
	projectRes := httptest.NewRecorder()
	handler.ServeHTTP(projectRes, projectReq)
	if projectRes.Code != http.StatusCreated {
		t.Fatalf("create project status=%d body=%s", projectRes.Code, projectRes.Body.String())
	}
	var project struct {
		ID    string `json:"id"`
		Links []struct {
			URL string `json:"url"`
		} `json:"links"`
	}
	if err := json.Unmarshal(projectRes.Body.Bytes(), &project); err != nil || project.ID == "" || len(project.Links) != 1 {
		t.Fatalf("project response missing data: %s", projectRes.Body.String())
	}
	updateBody := `{"name":"Compiler Platform","description":"Updated build service","category_id":"` + category.ID + `","links":[{"label":"Docs","url":"https://example.com/docs"}]}`
	projectUpdate := catalogRequest(http.MethodPut, "/api/v1/projects/"+project.ID, updateBody, session)
	projectUpdateRes := httptest.NewRecorder()
	handler.ServeHTTP(projectUpdateRes, projectUpdate)
	if projectUpdateRes.Code != http.StatusOK || !strings.Contains(projectUpdateRes.Body.String(), "Compiler Platform") {
		t.Fatalf("update project status=%d body=%s", projectUpdateRes.Code, projectUpdateRes.Body.String())
	}
	favoriteReq := catalogRequest(http.MethodPut, "/api/v1/projects/"+project.ID+"/favorite", `{"favorite":true}`, session)
	favoriteRes := httptest.NewRecorder()
	handler.ServeHTTP(favoriteRes, favoriteReq)
	if favoriteRes.Code != http.StatusNoContent {
		t.Fatalf("favorite status=%d body=%s", favoriteRes.Code, favoriteRes.Body.String())
	}
	listReq := catalogRequest(http.MethodGet, "/api/v1/projects?q=Compiler+Platform&category_id="+category.ID+"&favorites=true", "", session)
	listRes := httptest.NewRecorder()
	handler.ServeHTTP(listRes, listReq)
	if listRes.Code != http.StatusOK || !strings.Contains(listRes.Body.String(), project.ID) {
		t.Fatalf("filtered project list status=%d body=%s", listRes.Code, listRes.Body.String())
	}
	if got := listRes.Header().Get("X-Request-ID"); got == "" {
		t.Fatal("response did not include request ID")
	}
	deleteProject := catalogRequest(http.MethodDelete, "/api/v1/projects/"+project.ID, "", session)
	deleteProjectRes := httptest.NewRecorder()
	handler.ServeHTTP(deleteProjectRes, deleteProject)
	if deleteProjectRes.Code != http.StatusNoContent {
		t.Fatalf("delete project status=%d body=%s", deleteProjectRes.Code, deleteProjectRes.Body.String())
	}
	deleteCategory := catalogRequest(http.MethodDelete, "/api/v1/categories/"+category.ID, "", session)
	deleteCategoryRes := httptest.NewRecorder()
	handler.ServeHTTP(deleteCategoryRes, deleteCategory)
	if deleteCategoryRes.Code != http.StatusNoContent {
		t.Fatalf("delete category status=%d body=%s", deleteCategoryRes.Code, deleteCategoryRes.Body.String())
	}
	var audits int
	if err := db.QueryRow("SELECT count(*) FROM audit_logs WHERE actor_id=?", admin.ID).Scan(&audits); err != nil || audits != 7 {
		t.Fatalf("catalog mutation audit count=%d err=%v want=7", audits, err)
	}
	var targetType, targetID string
	if err := db.QueryRow("SELECT target_type,target_id FROM audit_logs WHERE actor_id=? AND action='project.create'", admin.ID).Scan(&targetType, &targetID); err != nil || targetType != "project" || targetID != project.ID {
		t.Fatalf("project audit target=%q/%q err=%v", targetType, targetID, err)
	}
}

func TestCatalogRoutesEnforceAuthRoleCSRFAndOrigin(t *testing.T) {
	db := testDatabase(t)
	service := auth.NewService(db)
	admin, err := service.BootstrapAdmin(t.Context(), "catalog-admin", "correct-horse-battery-staple")
	if err != nil {
		t.Fatal(err)
	}
	adminSession, err := service.NewSession(t.Context(), admin.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	viewerID := "d2719a4c-cf77-4c7d-8a8e-52163a58e579"
	if _, err := db.Exec("INSERT INTO users(id,username,password_hash,role,disabled,created_at,updated_at) VALUES(?,?,?,'viewer',0,1,1)", viewerID, "catalog-viewer", "unused"); err != nil {
		t.Fatal(err)
	}
	session, err := service.NewSession(t.Context(), viewerID, "")
	if err != nil {
		t.Fatal(err)
	}
	handler := newHandler(db)
	cases := []struct {
		name, method, path, body, origin, csrf string
		want                                   int
	}{
		{"anonymous", "GET", "/api/v1/projects", "", "", "", http.StatusUnauthorized},
		{"viewer read", "GET", "/api/v1/projects", "", "", "", http.StatusOK},
		{"viewer denied manage", "POST", "/api/v1/projects", `{"name":"Denied"}`, "http://localhost:8080", session.CSRFToken, http.StatusForbidden},
		{"missing csrf", "POST", "/api/v1/projects", `{"name":"Denied"}`, "http://localhost:8080", "", http.StatusForbidden},
		{"bad origin", "POST", "/api/v1/projects", `{"name":"Denied"}`, "https://attacker.invalid", session.CSRFToken, http.StatusForbidden},
		{"malformed json", "POST", "/api/v1/projects", `{"name":`, "http://localhost:8080", adminSession.CSRFToken, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.Host = "localhost:8080"
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			if tc.csrf != "" {
				req.Header.Set("X-CSRF-Token", tc.csrf)
			}
			if tc.name != "anonymous" {
				selected := session
				if tc.name == "malformed json" {
					selected = adminSession
				}
				req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: selected.Token})
			}
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, req)
			if res.Code != tc.want {
				t.Fatalf("status=%d want=%d body=%s", res.Code, tc.want, res.Body.String())
			}
		})
	}
}

func catalogRequest(method, path, body string, session auth.Session) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Host = "localhost:8080"
	r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session.Token})
	if method != "GET" {
		r.Header.Set("Origin", "http://localhost:8080")
		r.Header.Set("X-CSRF-Token", session.CSRFToken)
	}
	return r
}
