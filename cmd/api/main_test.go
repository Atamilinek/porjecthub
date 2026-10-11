package main

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/projecthub/projecthub/internal/migrate"
	"github.com/projecthub/projecthub/internal/store"
	"github.com/projecthub/projecthub/migrations"
)

func TestHealthEndpointReportsReadiness(t *testing.T) {
	db := testDatabase(t)
	response := httptest.NewRecorder()
	newHandler(db).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("GET /healthz status = %d, want %d", response.Code, http.StatusOK)
	}
}

func TestManagementRoutesRemainUnavailableUntilAuthenticationIsInstalled(t *testing.T) {
	db := testDatabase(t)
	response := httptest.NewRecorder()
	newHandler(db).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous GET /api/v1/projects status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func testDatabase(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	if err := migrate.Apply(t.Context(), db, migrations.FS); err != nil {
		db.Close()
		t.Fatalf("migrate.Apply() error = %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
