package store

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	_ "modernc.org/sqlite"
)

const (
	maxOpenConnections = 4
	busyTimeoutMS      = 5000
)

// Open returns a persistent SQLite database configured for concurrent readers,
// serialized writes, foreign-key enforcement, and crash-safe WAL journaling.
func Open(path string) (*sql.DB, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("database path is required")
	}
	if strings.HasPrefix(path, "file:") {
		return nil, errors.New("database path must be a filesystem path, not a SQLite URI")
	}
	if path != ":memory:" {
		dir := filepath.Dir(path)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			return nil, fmt.Errorf("create database file: %w", err)
		}
		if err := file.Close(); err != nil {
			return nil, fmt.Errorf("close database file: %w", err)
		}
		if err := os.Chmod(path, 0o600); err != nil {
			return nil, fmt.Errorf("restrict database file permissions: %w", err)
		}
	}

	dsn, err := dataSource(path)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}
	db.SetMaxOpenConns(maxOpenConnections)
	db.SetMaxIdleConns(maxOpenConnections)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect to sqlite database: %w", err)
	}
	return db, nil
}

func dataSource(path string) (string, error) {
	var u *url.URL
	if path == ":memory:" {
		u = &url.URL{Scheme: "file", Opaque: ":memory:"}
	} else {
		abs, err := filepath.Abs(path)
		if err != nil {
			return "", fmt.Errorf("resolve database path: %w", err)
		}
		p := filepath.ToSlash(abs)
		if runtime.GOOS == "windows" && len(p) >= 2 && p[1] == ':' {
			p = "/" + p
		}
		u = &url.URL{Scheme: "file", Path: p}
	}

	query := u.Query()
	query.Set("_journal_mode", "WAL")
	query.Set("_foreign_keys", "on")
	query.Set("_busy_timeout", fmt.Sprint(busyTimeoutMS))
	query.Set("_synchronous", "NORMAL")
	u.RawQuery = query.Encode()
	return u.String(), nil
}
