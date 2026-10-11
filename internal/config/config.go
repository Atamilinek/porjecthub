package config

import (
	"fmt"
	"net"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/projecthub/projecthub/internal/monitor"
)

type Config struct {
	DatabasePath      string
	ListenAddress     string
	ReadHeaderTimeout time.Duration
	ShutdownTimeout   time.Duration
	ProbeAllowCIDRs   []netip.Prefix
}

func Load() (Config, error) {
	return LoadFrom(func(key string) string { return strings.TrimSpace(os.Getenv(key)) })
}

func LoadFrom(lookup func(string) string) (Config, error) {
	if lookup == nil {
		return Config{}, fmt.Errorf("environment lookup is required")
	}
	cfg := Config{
		DatabasePath:      valueOrDefault(strings.TrimSpace(lookup("PROJECTHUB_DB_PATH")), "data/projecthub.db"),
		ListenAddress:     valueOrDefault(lookup("PROJECTHUB_LISTEN"), "127.0.0.1:8080"),
		ReadHeaderTimeout: 5 * time.Second,
		ShutdownTimeout:   10 * time.Second,
	}
	allowCIDRs := strings.TrimSpace(lookup("PROJECTHUB_PROBE_ALLOW_CIDRS"))
	if allowCIDRs != "" {
		prefixes, parseErr := monitor.ParseAllowedCIDRs(strings.Split(allowCIDRs, ","))
		if parseErr != nil {
			return Config{}, fmt.Errorf("PROJECTHUB_PROBE_ALLOW_CIDRS is invalid: %w", parseErr)
		}
		cfg.ProbeAllowCIDRs = prefixes
	}
	if cfg.DatabasePath == "" {
		return Config{}, fmt.Errorf("PROJECTHUB_DB_PATH cannot be empty")
	}
	host, portText, err := net.SplitHostPort(cfg.ListenAddress)
	if err != nil {
		return Config{}, fmt.Errorf("PROJECTHUB_LISTEN must be host:port: %w", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return Config{}, fmt.Errorf("PROJECTHUB_LISTEN port must be between 1 and 65535")
	}
	if strings.ContainsAny(host, " \t\r\n") {
		return Config{}, fmt.Errorf("PROJECTHUB_LISTEN host is invalid")
	}
	return cfg, nil
}

func valueOrDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
