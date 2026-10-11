package categories_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/projecthub/projecthub/internal/categories"
	"github.com/projecthub/projecthub/internal/migrate"
	"github.com/projecthub/projecthub/internal/store"
	"github.com/projecthub/projecthub/migrations"
)

func TestCategoryCreateUpdateListDelete(t *testing.T) {
	db := testDatabase(t)
	service := categories.NewService(db)
	created, err := service.Create(context.Background(), categories.CreateInput{Name: "  Engineering  ", Icon: "code", SortOrder: 2})
	if err != nil {
		t.Fatalf("create category: %v", err)
	}
	if created.Name != "Engineering" {
		t.Fatalf("created category name = %q, want trimmed value", created.Name)
	}
	updated, err := service.Update(context.Background(), created.ID, categories.UpdateInput{Name: "Platform", Icon: "server", SortOrder: 1})
	if err != nil {
		t.Fatalf("update category: %v", err)
	}
	if updated.Name != "Platform" || updated.Icon != "server" {
		t.Fatalf("updated category = %#v", updated)
	}
	items, err := service.List(context.Background())
	if err != nil {
		t.Fatalf("list categories: %v", err)
	}
	if len(items) != 1 || items[0].ID != created.ID {
		t.Fatalf("listed categories = %#v, want updated category", items)
	}
	if err := service.Delete(context.Background(), created.ID); err != nil {
		t.Fatalf("delete category: %v", err)
	}
	if _, err := service.Get(context.Background(), created.ID); !errors.Is(err, categories.ErrNotFound) {
		t.Fatalf("get deleted category error = %v, want ErrNotFound", err)
	}
}

func TestCategoryRejectsInvalidAndDuplicateNames(t *testing.T) {
	db := testDatabase(t)
	service := categories.NewService(db)
	if _, err := service.Create(context.Background(), categories.CreateInput{Name: "  "}); !errors.Is(err, categories.ErrInvalidInput) {
		t.Fatalf("Create(blank name) error = %v, want ErrInvalidInput", err)
	}
	if _, err := service.Create(context.Background(), categories.CreateInput{Name: "Engineering"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Create(context.Background(), categories.CreateInput{Name: "Engineering"}); !errors.Is(err, categories.ErrConflict) {
		t.Fatalf("Create(duplicate name) error = %v, want ErrConflict", err)
	}
	if _, err := service.Create(context.Background(), categories.CreateInput{Name: "engineering"}); !errors.Is(err, categories.ErrConflict) {
		t.Fatalf("Create(case-insensitive duplicate) error = %v, want ErrConflict", err)
	}
	if _, err := service.Get(context.Background(), "invalid-id"); !errors.Is(err, categories.ErrInvalidID) {
		t.Fatalf("Get(invalid ID) error = %v, want ErrInvalidID", err)
	}
}

func testDatabase(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "categories.db"))
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
