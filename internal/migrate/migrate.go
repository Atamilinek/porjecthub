package migrate

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var migrationFile = regexp.MustCompile(`^([0-9]{3,})_[a-z0-9_]+\.sql$`)

type migration struct {
	version int
	name    string
	content []byte
	hash    string
}

// Apply validates and transactionally applies each numbered SQL migration.
// Previously applied migrations are immutable and checked by SHA-256 digest.
func Apply(ctx context.Context, db *sql.DB, source fs.FS) error {
	if db == nil {
		return errors.New("migration database is required")
	}
	if source == nil {
		return errors.New("migration filesystem is required")
	}
	files, err := fs.ReadDir(source, ".")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}
	migrations := make([]migration, 0, len(files))
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".sql") {
			continue
		}
		match := migrationFile.FindStringSubmatch(file.Name())
		if match == nil {
			return fmt.Errorf("invalid migration filename %q", file.Name())
		}
		version, err := strconv.Atoi(match[1])
		if err != nil || version <= 0 {
			return fmt.Errorf("invalid migration version in %q", file.Name())
		}
		content, err := fs.ReadFile(source, file.Name())
		if err != nil {
			return fmt.Errorf("read migration %q: %w", file.Name(), err)
		}
		digest := sha256.Sum256(content)
		migrations = append(migrations, migration{
			version: version,
			name:    file.Name(),
			content: content,
			hash:    hex.EncodeToString(digest[:]),
		})
	}
	sort.Slice(migrations, func(i, j int) bool { return migrations[i].version < migrations[j].version })
	for i := 1; i < len(migrations); i++ {
		if migrations[i-1].version == migrations[i].version {
			return fmt.Errorf("duplicate migration version %d", migrations[i].version)
		}
	}

	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		name TEXT NOT NULL UNIQUE,
		checksum TEXT NOT NULL,
		applied_at INTEGER NOT NULL
	)`); err != nil {
		return fmt.Errorf("create migration ledger: %w", err)
	}
	known := make(map[int]migration, len(migrations))
	for _, item := range migrations {
		known[item.version] = item
	}
	rows, err := db.QueryContext(ctx, `SELECT version, name, checksum FROM schema_migrations ORDER BY version`)
	if err != nil {
		return fmt.Errorf("read migration ledger: %w", err)
	}
	for rows.Next() {
		var version int
		var name, checksum string
		if err := rows.Scan(&version, &name, &checksum); err != nil {
			rows.Close()
			return fmt.Errorf("scan migration ledger: %w", err)
		}
		item, exists := known[version]
		if !exists {
			rows.Close()
			return fmt.Errorf("applied migration %d (%s) is missing from the migration files", version, name)
		}
		if item.name != name || item.hash != checksum {
			rows.Close()
			return fmt.Errorf("applied migration %d has changed (recorded %s, current %s)", version, name, item.name)
		}
		delete(known, version)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("read migration ledger rows: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close migration ledger rows: %w", err)
	}

	for _, item := range migrations {
		if _, applied := known[item.version]; !applied {
			continue
		}
		if err := applyOne(ctx, db, item); err != nil {
			return err
		}
	}
	return nil
}

func applyOne(ctx context.Context, db *sql.DB, item migration) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration %s: %w", item.name, err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, string(item.content)); err != nil {
		return fmt.Errorf("execute migration %s: %w", item.name, err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version, name, checksum, applied_at) VALUES (?, ?, ?, ?)`, item.version, item.name, item.hash, time.Now().Unix()); err != nil {
		return fmt.Errorf("record migration %s: %w", item.name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration %s: %w", item.name, err)
	}
	return nil
}
