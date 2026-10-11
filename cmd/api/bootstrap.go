package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/projecthub/projecthub/internal/auth"
	"github.com/projecthub/projecthub/internal/config"
	"github.com/projecthub/projecthub/internal/migrate"
	"github.com/projecthub/projecthub/internal/store"
	"github.com/projecthub/projecthub/migrations"
)

func bootstrapAdmin(args []string) error {
	var username, passwordFile string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--username":
			if i+1 >= len(args) {
				return errors.New("--username requires a value")
			}
			i++
			username = args[i]
		case "--password-file":
			if i+1 >= len(args) {
				return errors.New("--password-file requires a value")
			}
			i++
			passwordFile = args[i]
		default:
			return fmt.Errorf("unknown bootstrap option %q", args[i])
		}
	}
	if username == "" || passwordFile == "" {
		return errors.New("usage: projecthub-api bootstrap-admin --username NAME --password-file PATH")
	}
	file, err := os.Open(passwordFile)
	if err != nil {
		return fmt.Errorf("open password file: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("inspect password file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("password file must be a regular file")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return errors.New("password file permissions must deny group and other access (use chmod 600)")
	}
	raw, err := io.ReadAll(io.LimitReader(file, 1025))
	if err != nil {
		return fmt.Errorf("read password file: %w", err)
	}
	if len(raw) > 1024 {
		return errors.New("password file exceeds 1024 bytes")
	}
	password := strings.TrimSuffix(strings.TrimSuffix(string(raw), "\n"), "\r")
	if err = auth.ValidatePassword(password); err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	db, err := store.Open(cfg.DatabasePath)
	if err != nil {
		return err
	}
	defer db.Close()
	if err = migrate.Apply(context.Background(), db, migrations.FS); err != nil {
		return err
	}
	service := auth.NewService(db)
	if _, err = service.BootstrapAdmin(context.Background(), username, password); err != nil {
		return err
	}
	return nil
}
