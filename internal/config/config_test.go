package config_test

import (
	"testing"

	"github.com/projecthub/projecthub/internal/config"
)

func TestLoadFromUsesLoopbackDefault(t *testing.T) {
	cfg, err := config.LoadFrom(func(string) string { return "" })
	if err != nil {
		t.Fatalf("LoadFrom() error = %v", err)
	}
	if cfg.ListenAddress != "127.0.0.1:8080" {
		t.Fatalf("ListenAddress = %q, want loopback default", cfg.ListenAddress)
	}
	if cfg.DatabasePath != "data/projecthub.db" {
		t.Fatalf("DatabasePath = %q, want data/projecthub.db", cfg.DatabasePath)
	}
}

func TestLoadFromRejectsInvalidListenAddress(t *testing.T) {
	_, err := config.LoadFrom(func(key string) string {
		if key == "PROJECTHUB_LISTEN" {
			return "not-an-address"
		}
		return ""
	})
	if err == nil {
		t.Fatal("LoadFrom() accepted an invalid listen address")
	}
}
