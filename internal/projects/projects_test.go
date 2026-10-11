package projects_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/projecthub/projecthub/internal/categories"
	"github.com/projecthub/projecthub/internal/migrate"
	"github.com/projecthub/projecthub/internal/projects"
	"github.com/projecthub/projecthub/internal/store"
	"github.com/projecthub/projecthub/migrations"
)

func TestCreateGetUpdateAndDeleteProjectWithLinks(t *testing.T) {
	db := testDatabase(t)
	categoriesService := categories.NewService(db)
	category, err := categoriesService.Create(context.Background(), categories.CreateInput{Name: "Engineering"})
	if err != nil {
		t.Fatalf("create category: %v", err)
	}
	service := projects.NewService(db)
	created, err := service.Create(context.Background(), projects.CreateInput{
		Name:        "Atlas",
		Description: "Build and deploy applications",
		CategoryID:  category.ID,
		Links: []projects.LinkInput{
			{Label: "Website", URL: "https://example.com"},
			{Label: "Repository", URL: "ssh://git@github.com/example/atlas.git"},
		},
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if len(created.Links) != 2 || created.CategoryID != category.ID {
		t.Fatalf("created project links/category = %d/%q, want 2/%q", len(created.Links), created.CategoryID, category.ID)
	}

	loaded, err := service.Get(context.Background(), created.ID, "")
	if err != nil {
		t.Fatalf("get project: %v", err)
	}
	if loaded.Name != "Atlas" || len(loaded.Links) != 2 {
		t.Fatalf("loaded project = %#v, want Atlas with two links", loaded)
	}

	updated, err := service.Update(context.Background(), created.ID, projects.UpdateInput{
		Name:       "Atlas Console",
		CategoryID: category.ID,
		Links:      []projects.LinkInput{{Label: "Dashboard", URL: "https://console.example.com"}},
	})
	if err != nil {
		t.Fatalf("update project: %v", err)
	}
	if updated.Name != "Atlas Console" || len(updated.Links) != 1 || updated.Links[0].Label != "Dashboard" {
		t.Fatalf("updated project = %#v", updated)
	}

	if err := service.Delete(context.Background(), created.ID); err != nil {
		t.Fatalf("delete project: %v", err)
	}
	if _, err := service.Get(context.Background(), created.ID, ""); !errors.Is(err, projects.ErrNotFound) {
		t.Fatalf("get deleted project error = %v, want ErrNotFound", err)
	}
}

func TestListSearchCategoryFavoritesAndPagination(t *testing.T) {
	db := testDatabase(t)
	ctx := context.Background()
	categoryService := categories.NewService(db)
	tools, err := categoryService.Create(ctx, categories.CreateInput{Name: "Tools"})
	if err != nil {
		t.Fatal(err)
	}
	apps, err := categoryService.Create(ctx, categories.CreateInput{Name: "Apps"})
	if err != nil {
		t.Fatal(err)
	}
	userID := "00000000-0000-4000-8000-000000000001"
	if _, err := db.Exec(`INSERT INTO users (id, username, password_hash, role, created_at, updated_at) VALUES (?, 'viewer', 'hash', 'viewer', 1, 1)`, userID); err != nil {
		t.Fatalf("insert viewer: %v", err)
	}
	service := projects.NewService(db)
	first, err := service.Create(ctx, projects.CreateInput{Name: "Atlas", CategoryID: tools.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Create(ctx, projects.CreateInput{Name: "Beacon", CategoryID: tools.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Create(ctx, projects.CreateInput{Name: "Arcade", CategoryID: apps.ID}); err != nil {
		t.Fatal(err)
	}
	if err := service.SetFavorite(ctx, userID, first.ID, true); err != nil {
		t.Fatalf("set favorite: %v", err)
	}

	filtered, err := service.List(ctx, userID, projects.ListFilter{
		Query:         "at",
		CategoryID:    tools.ID,
		FavoritesOnly: true,
		Limit:         10,
	})
	if err != nil {
		t.Fatalf("filtered list: %v", err)
	}
	if len(filtered) != 1 || filtered[0].ID != first.ID || !filtered[0].Favorite {
		t.Fatalf("filtered projects = %#v, want only favorite Atlas", filtered)
	}

	page, err := service.List(ctx, userID, projects.ListFilter{SortBy: "name", SortDirection: "asc", Limit: 1, Offset: 1})
	if err != nil {
		t.Fatalf("paginated list: %v", err)
	}
	if len(page) != 1 || page[0].Name != "Atlas" {
		t.Fatalf("second project page = %#v, want Atlas", page)
	}
	if err := service.Delete(ctx, first.ID); err != nil {
		t.Fatalf("delete favorite project: %v", err)
	}
	var orphanedFavorites int
	if err := db.QueryRow(`SELECT count(*) FROM user_preferences WHERE user_id = ? AND preference_key = ?`, userID, "favorite:"+first.ID).Scan(&orphanedFavorites); err != nil {
		t.Fatal(err)
	}
	if orphanedFavorites != 0 {
		t.Fatalf("orphaned favorite count = %d, want 0 after project deletion", orphanedFavorites)
	}
}

func TestProjectRejectsInvalidIdentifiersAndUnsafeLinks(t *testing.T) {
	db := testDatabase(t)
	service := projects.NewService(db)
	if _, err := service.Get(context.Background(), "not-a-uuid", ""); !errors.Is(err, projects.ErrInvalidID) {
		t.Fatalf("Get(invalid ID) error = %v, want ErrInvalidID", err)
	}
	for i, unsafeURL := range []string{
		"javascript:alert(1)",
		"file:///etc/passwd",
		"docker://var/run/docker.sock",
		"https://operator:secret@example.com",
		"https://example.com/%zz",
	} {
		_, err := service.Create(context.Background(), projects.CreateInput{
			Name:  fmt.Sprintf("Unsafe %d", i),
			Links: []projects.LinkInput{{Label: "Open", URL: unsafeURL}},
		})
		if !errors.Is(err, projects.ErrInvalidInput) {
			t.Errorf("Create(unsafe URL %q) error = %v, want ErrInvalidInput", unsafeURL, err)
		}
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM projects`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("projects persisted after invalid input = %d, want 0", count)
	}
}

func TestProjectRejectsDuplicateLinksAndMissingCategory(t *testing.T) {
	db := testDatabase(t)
	service := projects.NewService(db)
	_, err := service.Create(context.Background(), projects.CreateInput{
		Name: "Duplicate links",
		Links: []projects.LinkInput{
			{Label: "One", URL: "https://example.com"},
			{Label: "Two", URL: "https://example.com"},
		},
	})
	if !errors.Is(err, projects.ErrInvalidInput) {
		t.Fatalf("Create(duplicate links) error = %v, want ErrInvalidInput", err)
	}
	_, err = service.Create(context.Background(), projects.CreateInput{Name: "Missing category", CategoryID: "00000000-0000-4000-8000-000000000002"})
	if !errors.Is(err, projects.ErrCategoryNotFound) {
		t.Fatalf("Create(missing category) error = %v, want ErrCategoryNotFound", err)
	}
}

func TestProjectNamesAreCaseInsensitiveUnique(t *testing.T) {
	service := projects.NewService(testDatabase(t))
	if _, err := service.Create(context.Background(), projects.CreateInput{Name: "Docs Portal"}); err != nil {
		t.Fatalf("create first project: %v", err)
	}
	if _, err := service.Create(context.Background(), projects.CreateInput{Name: "docs portal"}); !errors.Is(err, projects.ErrConflict) {
		t.Fatalf("create case-insensitive duplicate error = %v, want ErrConflict", err)
	}
}

func testDatabase(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "projects.db"))
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
