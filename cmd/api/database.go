package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/projecthub/projecthub/internal/config"
	"github.com/projecthub/projecthub/internal/migrate"
	"github.com/projecthub/projecthub/internal/store"
	"github.com/projecthub/projecthub/migrations"
)

func runDatabaseCommand(command string, args []string) error {
	if len(args) != 2 || args[0] != "--file" || strings.TrimSpace(args[1]) == "" {
		return fmt.Errorf("usage: projecthub-api %s --file PATH", command)
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if command == "backup" {
		return createBackup(cfg.DatabasePath, args[1])
	}
	if command == "restore" {
		return restoreBackup(args[1], cfg.DatabasePath)
	}
	return errors.New("unsupported database command")
}

func createBackup(databasePath, outputPath string) error {
	dbAbs, err := filepath.Abs(databasePath)
	if err != nil {
		return err
	}
	outAbs, err := filepath.Abs(outputPath)
	if err != nil {
		return err
	}
	if filepath.Clean(dbAbs) == filepath.Clean(outAbs) {
		return errors.New("backup output must differ from the live database")
	}
	if info, statErr := os.Stat(dbAbs); statErr != nil {
		return fmt.Errorf("inspect live database: %w", statErr)
	} else if !info.Mode().IsRegular() {
		return errors.New("live database must be a regular file")
	}
	if _, err := os.Stat(outAbs); err == nil {
		return errors.New("backup output already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(outAbs), 0o700); err != nil {
		return err
	}
	db, err := store.Open(databasePath)
	if err != nil {
		return err
	}
	defer db.Close()
	quoted := strings.ReplaceAll(outAbs, "'", "''")
	if _, err = db.ExecContext(context.Background(), "VACUUM INTO '"+quoted+"'"); err != nil {
		return fmt.Errorf("create consistent SQLite backup: %w", err)
	}
	if err = os.Chmod(outAbs, 0o600); err != nil {
		_ = os.Remove(outAbs)
		return err
	}
	return nil
}

func restoreBackup(inputPath, databasePath string) error {
	inAbs, err := filepath.Abs(inputPath)
	if err != nil {
		return err
	}
	dbAbs, err := filepath.Abs(databasePath)
	if err != nil {
		return err
	}
	if filepath.Clean(inAbs) == filepath.Clean(dbAbs) {
		return errors.New("restore source must differ from the live database")
	}
	info, err := os.Stat(inAbs)
	if err != nil {
		return fmt.Errorf("inspect backup: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("restore source must be a regular file")
	}
	if err = os.MkdirAll(filepath.Dir(dbAbs), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dbAbs), ".projecthub-restore-*.db")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err = tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	source, err := os.Open(inAbs)
	if err != nil {
		tmp.Close()
		return err
	}
	_, copyErr := io.Copy(tmp, source)
	closeSourceErr := source.Close()
	closeTmpErr := tmp.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeSourceErr != nil {
		return closeSourceErr
	}
	if closeTmpErr != nil {
		return closeTmpErr
	}
	validationDB, err := store.Open(tmpPath)
	if err != nil {
		return fmt.Errorf("open staged restore: %w", err)
	}
	if err = migrate.Apply(context.Background(), validationDB, migrations.FS); err == nil {
		var integrity string
		err = validationDB.QueryRowContext(context.Background(), "PRAGMA integrity_check").Scan(&integrity)
		if err == nil && integrity != "ok" {
			err = fmt.Errorf("SQLite integrity check failed: %s", integrity)
		}
	}
	closeErr := validationDB.Close()
	if err != nil {
		return fmt.Errorf("validate staged restore: %w", err)
	}
	if closeErr != nil {
		return closeErr
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if removeErr := os.Remove(tmpPath + suffix); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return fmt.Errorf("clean staged SQLite sidecar: %w", removeErr)
		}
	}
	if err = os.Chmod(tmpPath, 0o600); err != nil {
		return err
	}
	var movedSidecars []string
	for _, suffix := range []string{"-wal", "-shm"} {
		sidecar := dbAbs + suffix
		if _, statErr := os.Lstat(sidecar); errors.Is(statErr, os.ErrNotExist) {
			continue
		} else if statErr != nil {
			return statErr
		}
		stagedSidecar := sidecar + ".staged"
		if _, statErr := os.Lstat(stagedSidecar); statErr == nil {
			return fmt.Errorf("staged sidecar already exists: %s", stagedSidecar)
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return statErr
		}
		if err = os.Rename(sidecar, stagedSidecar); err != nil {
			for i := len(movedSidecars) - 1; i >= 0; i-- {
				_ = os.Rename(movedSidecars[i]+".staged", movedSidecars[i])
			}
			return fmt.Errorf("stage SQLite sidecar %s: %w", suffix, err)
		}
		movedSidecars = append(movedSidecars, sidecar)
	}
	if err = os.Rename(tmpPath, dbAbs); err != nil {
		for i := len(movedSidecars) - 1; i >= 0; i-- {
			_ = os.Rename(movedSidecars[i]+".staged", movedSidecars[i])
		}
		return fmt.Errorf("install restored database (stop Project Hub first): %w", err)
	}
	for _, sidecar := range movedSidecars {
		if err = os.Remove(sidecar + ".staged"); err != nil {
			return fmt.Errorf("remove stale SQLite sidecar after restore: %w", err)
		}
	}
	return nil
}
