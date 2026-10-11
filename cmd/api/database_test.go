package main

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/projecthub/projecthub/internal/migrate"
	"github.com/projecthub/projecthub/internal/store"
	"github.com/projecthub/projecthub/migrations"
)

func TestBackupRestoreRoundTripAndIntegrity(t *testing.T) {
	dir := t.TempDir()
	live := filepath.Join(dir, "live.db")
	backup := filepath.Join(dir, "backup.db")
	db, err := store.Open(live)
	if err != nil {
		t.Fatal(err)
	}
	if err = migrate.Apply(context.Background(), db, migrations.FS); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`CREATE TABLE restore_probe (value TEXT NOT NULL); INSERT INTO restore_probe VALUES ('before')`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if err = createBackup(live, backup); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(backup); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("backup mode/stat = %v, %v", info, err)
	}
	db, err = store.Open(live)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE restore_probe SET value='after'`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if err = os.WriteFile(live+"-wal", []byte("stale wal"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(live+"-shm", []byte("stale shm"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err = restoreBackup(backup, live); err != nil {
		t.Fatal(err)
	}
	for _, sidecar := range []string{live + "-wal", live + "-shm"} {
		if _, err = os.Stat(sidecar); !os.IsNotExist(err) {
			t.Fatalf("stale sidecar remains: %s (%v)", sidecar, err)
		}
	}
	db, err = store.Open(live)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var got string
	if err = db.QueryRow(`SELECT value FROM restore_probe`).Scan(&got); err != nil || got != "before" {
		t.Fatalf("restored value = %q, %v", got, err)
	}
	var integrity string
	if err = db.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("integrity = %q, %v", integrity, err)
	}
}

func TestBackupRejectsMissingLiveDatabase(t *testing.T) {
	dir := t.TempDir()
	if err := createBackup(filepath.Join(dir, "missing.db"), filepath.Join(dir, "backup.db")); err == nil {
		t.Fatal("expected missing database rejection")
	}
}

func TestDatabaseCommandWiring(t *testing.T) {
	dir := t.TempDir()
	live := filepath.Join(dir, "live.db")
	backup := filepath.Join(dir, "backup.db")
	t.Setenv("PROJECTHUB_DB_PATH", live)
	if err := runDatabaseCommand("backup", []string{"--file", backup}); err == nil {
		t.Fatal("expected backup of missing database to fail")
	}
	db, err := store.Open(live)
	if err != nil {
		t.Fatal(err)
	}
	if err = migrate.Apply(context.Background(), db, migrations.FS); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`CREATE TABLE cli_probe (value TEXT); INSERT INTO cli_probe VALUES ('saved')`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if err = runDatabaseCommand("backup", []string{"--file", backup}); err != nil {
		t.Fatal(err)
	}
	if err = runDatabaseCommand("restore", []string{"--file", backup}); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreRejectsInvalidSourceWithoutReplacingDatabase(t *testing.T) {
	dir := t.TempDir()
	live := filepath.Join(dir, "live.db")
	invalid := filepath.Join(dir, "invalid.db")
	db, err := store.Open(live)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`CREATE TABLE preserve_me (value TEXT); INSERT INTO preserve_me VALUES ('safe')`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if err = os.WriteFile(invalid, []byte("not a sqlite database"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err = restoreBackup(invalid, live); err == nil {
		t.Fatal("expected invalid source rejection")
	}
	db, err = sql.Open("sqlite", "file:"+live)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var got string
	if err = db.QueryRow(`SELECT value FROM preserve_me`).Scan(&got); err != nil || got != "safe" {
		t.Fatalf("live database changed: %q %v", got, err)
	}
}
