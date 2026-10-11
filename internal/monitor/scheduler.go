package monitor

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/projecthub/projecthub/internal/events"
)

type Scheduler struct {
	service     *Service
	broker      *events.Broker
	tick        time.Duration
	retention   time.Duration
	lastCleanup time.Time
	mu          sync.Mutex
	inFlight    map[string]bool
	wg          sync.WaitGroup
}
type StateEvent struct {
	CheckID   string `json:"check_id"`
	ProjectID string `json:"project_id"`
	State     State  `json:"state"`
	LatencyMS int64  `json:"latency_ms"`
	CheckedAt int64  `json:"checked_at"`
}

func NewScheduler(service *Service, broker *events.Broker, tick time.Duration) *Scheduler {
	if tick <= 0 {
		tick = time.Second
	}
	return &Scheduler{service: service, broker: broker, tick: tick, retention: 30 * 24 * time.Hour, inFlight: make(map[string]bool)}
}
func (s *Scheduler) Run(ctx context.Context) error {
	ticker := time.NewTicker(s.tick)
	defer ticker.Stop()
	s.RunDue(ctx)
	for {
		select {
		case <-ctx.Done():
			s.wg.Wait()
			return ctx.Err()
		case <-ticker.C:
			s.RunDue(ctx)
		}
	}
}
func (s *Scheduler) RunDue(ctx context.Context) int {
	now := time.Now().Unix()
	s.mu.Lock()
	cleanupDue := s.lastCleanup.IsZero() || time.Since(s.lastCleanup) >= 24*time.Hour
	if cleanupDue {
		s.lastCleanup = time.Now()
	}
	s.mu.Unlock()
	if cleanupDue {
		_, _ = s.service.CleanupSamples(ctx, time.Now().Add(-s.retention))
	}
	rows, err := s.service.db.QueryContext(ctx, "SELECT id FROM health_checks WHERE enabled=1 AND (last_checked_at IS NULL OR last_checked_at + interval_seconds <= ?) ORDER BY COALESCE(last_checked_at,0),id", now)
	if err != nil {
		return 0
	}
	idsToRun := make([]string, 0)
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			idsToRun = append(idsToRun, id)
		}
	}
	_ = rows.Close()
	started := 0
	for _, id := range idsToRun {
		s.mu.Lock()
		if s.inFlight[id] {
			s.mu.Unlock()
			continue
		}
		s.inFlight[id] = true
		s.wg.Add(1)
		s.mu.Unlock()
		started++
		go s.run(ctx, id)
	}
	return started
}
func (s *Scheduler) run(ctx context.Context, id string) {
	defer s.wg.Done()
	defer func() { s.mu.Lock(); delete(s.inFlight, id); s.mu.Unlock() }()
	check, err := s.service.Get(ctx, id)
	if err != nil {
		return
	}
	next, err := s.service.RunOnce(ctx, id)
	if err != nil {
		return
	}
	if s.broker != nil {
		sample, err := s.service.Samples(ctx, id, 1)
		if err == nil && len(sample) > 0 {
			latency := int64(0)
			if sample[0].LatencyMS != nil {
				latency = *sample[0].LatencyMS
			}
			event := StateEvent{CheckID: id, ProjectID: next.ProjectID, State: next.State, LatencyMS: latency, CheckedAt: sample[0].CheckedAt}
			data, _ := json.Marshal(event)
			s.broker.Publish("health.sample", data)
			if check.State != next.State {
				s.broker.Publish("health.state", data)
			}
		}
	}
}
