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

func TestLoadFromParsesPrivateProbeAllowCIDRs(t *testing.T) {
	cfg, err := config.LoadFrom(func(key string) string {
		if key == "PROJECTHUB_PROBE_ALLOW_CIDRS" {
			return "10.8.0.0/24, fc00::/8"
		}
		return ""
	})
	if err != nil || len(cfg.ProbeAllowCIDRs) != 2 {
		t.Fatalf("LoadFrom() allow CIDRs=%v err=%v, want two private prefixes", cfg.ProbeAllowCIDRs, err)
	}
}

func TestLoadFromRejectsBroadOrPublicProbeCIDRs(t *testing.T) {
	for _, value := range []string{"0.0.0.0/0", "10.0.0.0/7", "8.8.8.0/24"} {
		if _, err := config.LoadFrom(func(key string) string {
			if key == "PROJECTHUB_PROBE_ALLOW_CIDRS" {
				return value
			}
			return ""
		}); err == nil {
			t.Errorf("LoadFrom() accepted unsafe probe CIDR %q", value)
		}
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
