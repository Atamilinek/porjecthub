package monitor

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/projecthub/projecthub/internal/ids"
)

var (
	ErrInvalidCheck        = errors.New("invalid health check")
	ErrCheckNotFound       = errors.New("health check not found")
	ErrProjectNotFound     = errors.New("project not found")
	ErrAgentTargetNotFound = errors.New("agent target not found")
	ErrAgentUnavailable    = errors.New("agent unavailable")
)

type Kind string

const (
	KindHTTP  Kind = "http"
	KindTCP   Kind = "tcp"
	KindAgent Kind = "agent"
)

type HealthCheck struct {
	ID                string `json:"id"`
	ProjectID         string `json:"project_id"`
	Kind              Kind   `json:"kind"`
	Target            string `json:"target"`
	IntervalSeconds   int    `json:"interval_seconds"`
	TimeoutSeconds    int    `json:"timeout_seconds"`
	FailureThreshold  int    `json:"failure_threshold"`
	RecoveryThreshold int    `json:"recovery_threshold"`
	Enabled           bool   `json:"enabled"`
	State             State  `json:"state"`
	FailureCount      int    `json:"failure_count"`
	RecoveryCount     int    `json:"recovery_count"`
	LastCheckedAt     *int64 `json:"last_checked_at"`
}
type Sample struct {
	ID        string `json:"id"`
	CheckID   string `json:"check_id"`
	State     State  `json:"state"`
	LatencyMS *int64 `json:"latency_ms"`
	Detail    string `json:"detail"`
	CheckedAt int64  `json:"checked_at"`
}
type CreateInput struct {
	Kind                                                                 Kind
	Target                                                               string
	IntervalSeconds, TimeoutSeconds, FailureThreshold, RecoveryThreshold int
	Enabled                                                              bool
}
type Checker interface {
	Check(context.Context, HealthCheck) (ProbeResult, error)
}
type Service struct {
	db        *sql.DB
	checker   Checker
	now       func() time.Time
	semaphore chan struct{}
}

func NewService(db *sql.DB, checker Checker, maxConcurrency int) *Service {
	if maxConcurrency < 1 {
		maxConcurrency = 10
	}
	return &Service{db: db, checker: checker, now: time.Now, semaphore: make(chan struct{}, maxConcurrency)}
}

func (s *Service) Create(ctx context.Context, projectID string, input CreateInput) (HealthCheck, error) {
	if !ids.IsUUID(projectID) {
		return HealthCheck{}, ErrInvalidCheck
	}
	if err := normalizeCheck(&input); err != nil {
		return HealthCheck{}, err
	}
	if input.Kind == KindAgent {
		var mappedProject string
		if err := s.db.QueryRowContext(ctx, "SELECT project_id FROM service_targets WHERE id=?", input.Target).Scan(&mappedProject); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return HealthCheck{}, ErrAgentTargetNotFound
			}
			return HealthCheck{}, err
		}
		if mappedProject != projectID {
			return HealthCheck{}, ErrAgentTargetNotFound
		}
	}
	var exists int
	if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM projects WHERE id=?", projectID).Scan(&exists); err != nil {
		return HealthCheck{}, err
	}
	if exists == 0 {
		return HealthCheck{}, ErrProjectNotFound
	}
	id, err := ids.NewUUID()
	if err != nil {
		return HealthCheck{}, err
	}
	now := s.now().Unix()
	enabled := 0
	if input.Enabled {
		enabled = 1
	}
	_, err = s.db.ExecContext(ctx, "INSERT INTO health_checks(id,project_id,kind,target,interval_seconds,timeout_seconds,failure_threshold,recovery_threshold,enabled,created_at,updated_at,current_state) VALUES(?,?,?,?,?,?,?,?,?,?,?,'unknown')", id, projectID, input.Kind, input.Target, input.IntervalSeconds, input.TimeoutSeconds, input.FailureThreshold, input.RecoveryThreshold, enabled, now, now)
	if err != nil {
		return HealthCheck{}, fmt.Errorf("create health check: %w", err)
	}
	return s.Get(ctx, id)
}
func (s *Service) Get(ctx context.Context, id string) (HealthCheck, error) {
	var item HealthCheck
	var enabled int
	var last sql.NullInt64
	err := s.db.QueryRowContext(ctx, "SELECT id,project_id,kind,target,interval_seconds,timeout_seconds,failure_threshold,recovery_threshold,enabled,current_state,failure_count,recovery_count,last_checked_at FROM health_checks WHERE id=?", id).Scan(&item.ID, &item.ProjectID, &item.Kind, &item.Target, &item.IntervalSeconds, &item.TimeoutSeconds, &item.FailureThreshold, &item.RecoveryThreshold, &enabled, &item.State, &item.FailureCount, &item.RecoveryCount, &last)
	if errors.Is(err, sql.ErrNoRows) {
		return HealthCheck{}, ErrCheckNotFound
	}
	if err != nil {
		return HealthCheck{}, err
	}
	item.Enabled = enabled != 0
	if last.Valid {
		item.LastCheckedAt = &last.Int64
	}
	return item, nil
}
func (s *Service) List(ctx context.Context, projectID string) ([]HealthCheck, error) {
	query := "SELECT id FROM health_checks ORDER BY created_at,id"
	args := []any{}
	if projectID != "" {
		if !ids.IsUUID(projectID) {
			return nil, ErrInvalidCheck
		}
		query = "SELECT id FROM health_checks WHERE project_id=? ORDER BY created_at,id"
		args = append(args, projectID)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]HealthCheck, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		item, err := s.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
func (s *Service) Samples(ctx context.Context, checkID string, limit int) ([]Sample, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, "SELECT id,check_id,status,latency_ms,detail,checked_at FROM health_samples WHERE check_id=? ORDER BY checked_at DESC,rowid DESC LIMIT ?", checkID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Sample, 0)
	for rows.Next() {
		var v Sample
		var latency sql.NullInt64
		if err := rows.Scan(&v.ID, &v.CheckID, &v.State, &latency, &v.Detail, &v.CheckedAt); err != nil {
			return nil, err
		}
		if latency.Valid {
			v.LatencyMS = &latency.Int64
		}
		items = append(items, v)
	}
	return items, rows.Err()
}
func (s *Service) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM health_checks WHERE id=?", id)
	if err != nil {
		return err
	}
	count, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrCheckNotFound
	}
	return nil
}
func (s *Service) CleanupSamples(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, "DELETE FROM health_samples WHERE checked_at < ?", before.Unix())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
func (s *Service) RunOnce(ctx context.Context, id string) (HealthCheck, error) {
	select {
	case s.semaphore <- struct{}{}:
		defer func() { <-s.semaphore }()
	case <-ctx.Done():
		return HealthCheck{}, ctx.Err()
	}
	check, err := s.Get(ctx, id)
	if err != nil {
		return HealthCheck{}, err
	}
	if !check.Enabled {
		return check, nil
	}
	probeCtx, cancel := context.WithTimeout(ctx, time.Duration(check.TimeoutSeconds)*time.Second)
	defer cancel()
	var result ProbeResult
	var probeErr error
	if s.checker == nil {
		probeErr = errors.New("health checker unavailable")
	} else {
		result, probeErr = s.checker.Check(probeCtx, check)
	}
	agentDown := check.Kind == KindAgent && errors.Is(probeErr, ErrAgentUnavailable)
	return s.record(ctx, check, result, probeErr, agentDown)
}
func (s *Service) record(ctx context.Context, check HealthCheck, result ProbeResult, probeErr error, agentDown bool) (HealthCheck, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return HealthCheck{}, err
	}
	defer tx.Rollback()
	current, failures, recoveries := check.State, check.FailureCount, check.RecoveryCount
	transition := Transition(current, probeErr == nil, failures, recoveries, check.FailureThreshold, check.RecoveryThreshold, agentDown)
	now := s.now().Unix()
	latency := result.Latency.Milliseconds()
	var detail string
	if agentDown {
		detail = "agent unreachable"
	} else if probeErr != nil {
		detail = result.Detail
		if detail == "" {
			detail = "probe failed"
		}
	} else if result.StatusCode != 0 {
		detail = fmt.Sprintf("HTTP %d", result.StatusCode)
	}
	sampleID, err := ids.NewUUID()
	if err != nil {
		return HealthCheck{}, err
	}
	var latencyValue any
	if result.Latency > 0 {
		latencyValue = latency
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO health_samples(id,check_id,status,latency_ms,detail,checked_at) VALUES(?,?,?,?,?,?)", sampleID, check.ID, transition.State, latencyValue, detail, now); err != nil {
		return HealthCheck{}, fmt.Errorf("save health sample: %w", err)
	}
	if _, err = tx.ExecContext(ctx, "UPDATE health_checks SET current_state=?,failure_count=?,recovery_count=?,last_checked_at=?,updated_at=? WHERE id=?", transition.State, transition.Failures, transition.Recoveries, now, now, check.ID); err != nil {
		return HealthCheck{}, err
	}
	if err = tx.Commit(); err != nil {
		return HealthCheck{}, err
	}
	return s.Get(ctx, check.ID)
}

func normalizeCheck(input *CreateInput) error {
	if input.Kind != KindHTTP && input.Kind != KindTCP && input.Kind != KindAgent {
		return ErrInvalidCheck
	}
	input.Target = strings.TrimSpace(input.Target)
	if input.Target == "" || len(input.Target) > 2048 || strings.ContainsAny(input.Target, "\r\n\x00") {
		return ErrInvalidCheck
	}
	switch input.Kind {
	case KindHTTP:
		if _, err := validateHTTPURL(input.Target); err != nil {
			return ErrInvalidCheck
		}
	case KindTCP:
		host, portText, err := net.SplitHostPort(input.Target)
		port, portErr := strconv.Atoi(portText)
		if err != nil || host == "" || portErr != nil || port < 1 || port > 65535 {
			return ErrInvalidCheck
		}
	case KindAgent:
		if !ids.IsUUID(input.Target) {
			return ErrInvalidCheck
		}
	}
	if input.IntervalSeconds == 0 {
		input.IntervalSeconds = 30
	}
	if input.TimeoutSeconds == 0 {
		input.TimeoutSeconds = 5
	}
	if input.FailureThreshold == 0 {
		input.FailureThreshold = 3
	}
	if input.RecoveryThreshold == 0 {
		input.RecoveryThreshold = 2
	}
	if input.IntervalSeconds < 5 || input.IntervalSeconds > 3600 || input.TimeoutSeconds < 1 || input.TimeoutSeconds > 60 || input.FailureThreshold < 1 || input.FailureThreshold > 20 || input.RecoveryThreshold < 1 || input.RecoveryThreshold > 20 {
		return ErrInvalidCheck
	}
	return nil
}

type DefaultChecker struct {
	HTTP  HTTPProbe
	TCP   *SafeDialer
	Agent AgentProbe
}

func NewDefaultChecker(allowed []netip.Prefix) *DefaultChecker {
	return &DefaultChecker{HTTP: HTTPProbe{Client: NewHTTPClient(allowed)}, TCP: NewSafeDialer(nil, nil, allowed)}
}

type AgentProbe interface {
	CheckAgent(context.Context, string) (ProbeResult, error)
}

func (c *DefaultChecker) Check(ctx context.Context, check HealthCheck) (ProbeResult, error) {
	switch check.Kind {
	case KindHTTP:
		return c.HTTP.Check(ctx, check.Target)
	case KindTCP:
		dialer := c.TCP
		if dialer == nil {
			dialer = NewSafeDialer(nil, nil, nil)
		}
		started := time.Now()
		conn, err := dialer.DialContext(ctx, "tcp", check.Target)
		latency := time.Since(started)
		if err != nil {
			return ProbeResult{Latency: latency, Detail: "TCP dial failed"}, err
		}
		_ = conn.Close()
		return ProbeResult{OK: true, Latency: latency}, nil
	case KindAgent:
		if c.Agent == nil {
			return ProbeResult{}, ErrAgentUnavailable
		}
		return c.Agent.CheckAgent(ctx, check.Target)
	default:
		return ProbeResult{}, ErrInvalidCheck
	}
}

func ParseAllowedCIDRs(values []string) ([]netip.Prefix, error) {
	result := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
		if err != nil || !isNarrowPrivatePrefix(prefix) {
			return nil, fmt.Errorf("probe allow CIDR must be a private network: %q", value)
		}
		result = append(result, prefix.Masked())
	}
	return result, nil
}

func isNarrowPrivatePrefix(prefix netip.Prefix) bool {
	if !prefix.IsValid() {
		return false
	}
	for _, private := range mustPrefixes("10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "fc00::/7") {
		if prefix.Bits() >= private.Bits() && private.Contains(prefix.Addr()) {
			return true
		}
	}
	return false
}
