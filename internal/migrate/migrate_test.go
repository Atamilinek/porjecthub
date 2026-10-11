package migrate_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/projecthub/projecthub/internal/migrate"
	"github.com/projecthub/projecthub/internal/store"
	"github.com/projecthub/projecthub/migrations"
)

func TestApplyCreatesSchemaAndIsIdempotent(t *testing.T) {
	db := openDatabase(t)
	ctx := context.Background()
	if err := migrate.Apply(ctx, db, migrations.FS); err != nil {
		t.Fatalf("first Apply() error = %v", err)
	}
	if err := migrate.Apply(ctx, db, migrations.FS); err != nil {
		t.Fatalf("second Apply() error = %v", err)
	}

	var tables int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='projects'`).Scan(&tables); err != nil {
		t.Fatalf("query projects table: %v", err)
	}
	if tables != 1 {
		t.Fatalf("projects table count = %d, want 1", tables)
	}

	var applied int
	if err := db.QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&applied); err != nil {
		t.Fatalf("query migration ledger: %v", err)
	}
	if applied != 4 {
		t.Fatalf("applied migration count = %d, want 4", applied)
	}
}

func TestMigrationDataPersistsAfterReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "persistent.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatalf("first Open() error = %v", err)
	}
	if err := migrate.Apply(context.Background(), db, migrations.FS); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if _, err := db.Exec(`INSERT INTO categories (id, name, created_at, updated_at) VALUES ('cat-test', 'Development', 1, 1)`); err != nil {
		db.Close()
		t.Fatalf("insert category: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}

	reopened, err := store.Open(path)
	if err != nil {
		t.Fatalf("reopen database: %v", err)
	}
	defer reopened.Close()
	var name string
	if err := reopened.QueryRow(`SELECT name FROM categories WHERE id = ?`, "cat-test").Scan(&name); err != nil {
		t.Fatalf("read persisted category: %v", err)
	}
	if name != "Development" {
		t.Fatalf("persisted category = %q, want Development", name)
	}
}

func TestApplyRejectsChangedAppliedMigration(t *testing.T) {
	db := openDatabase(t)
	ctx := context.Background()
	original := fstest.MapFS{"001_initial.sql": &fstest.MapFile{Data: []byte("CREATE TABLE example (id TEXT);")}}
	if err := migrate.Apply(ctx, db, original); err != nil {
		t.Fatalf("apply original migration: %v", err)
	}
	changed := fstest.MapFS{"001_initial.sql": &fstest.MapFile{Data: []byte("CREATE TABLE example (id INTEGER);")}}
	if err := migrate.Apply(ctx, db, changed); err == nil {
		t.Fatal("Apply() accepted a changed migration that was already applied")
	}
}

func TestApplyRollsBackFailedMigration(t *testing.T) {
	db := openDatabase(t)
	source := fstest.MapFS{"001_broken.sql": &fstest.MapFile{Data: []byte("CREATE TABLE temporary_table (id TEXT); INSERT INTO missing_table VALUES ('x');")}}
	if err := migrate.Apply(context.Background(), db, source); err == nil {
		t.Fatal("Apply() succeeded for a migration containing invalid SQL")
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='temporary_table'`).Scan(&count); err != nil {
		t.Fatalf("check rolled-back DDL: %v", err)
	}
	if count != 0 {
		t.Fatalf("temporary_table count = %d, want 0 after rollback", count)
	}
	if err := db.QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatalf("check migration ledger: %v", err)
	}
	if count != 0 {
		t.Fatalf("migration ledger count = %d, want 0 after rollback", count)
	}
}

func openDatabase(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
