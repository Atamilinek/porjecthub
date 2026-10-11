package monitor

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/projecthub/projecthub/internal/events"
	"github.com/projecthub/projecthub/internal/migrate"
	"github.com/projecthub/projecthub/internal/projects"
	"github.com/projecthub/projecthub/internal/store"
	"github.com/projecthub/projecthub/migrations"
)

type checkerFunc func(context.Context, HealthCheck) (ProbeResult, error)

var monitorProjectCounter atomic.Int64

func (f checkerFunc) Check(ctx context.Context, c HealthCheck) (ProbeResult, error) { return f(ctx, c) }

func TestRunOncePersistsFailureRecoverySamplesAndRetention(t *testing.T) {
	db := monitorDB(t)
	projectID := createMonitorProject(t, db)
	steps := []error{nil, errors.New("connection refused"), errors.New("connection refused"), nil, nil}
	index := 0
	service := NewService(db, checkerFunc(func(context.Context, HealthCheck) (ProbeResult, error) {
		err := steps[index]
		index++
		return ProbeResult{StatusCode: 200, Latency: 25 * time.Millisecond}, err
	}), 2)
	check, err := service.Create(t.Context(), projectID, CreateInput{Kind: KindHTTP, Target: "https://status.example.com/health", FailureThreshold: 2, RecoveryThreshold: 2, Enabled: true})
	if err != nil {
		t.Fatalf("Create(): %v", err)
	}
	expected := []State{StateHealthy, StateDegraded, StateOffline, StateDegraded, StateHealthy}
	for i, want := range expected {
		check, err = service.RunOnce(t.Context(), check.ID)
		if err != nil {
			t.Fatalf("RunOnce(%d): %v", i, err)
		}
		if check.State != want {
			t.Fatalf("RunOnce(%d) state=%s want=%s", i, check.State, want)
		}
	}
	samples, err := service.Samples(t.Context(), check.ID, 20)
	if err != nil || len(samples) != 5 {
		t.Fatalf("samples=%d err=%v want 5", len(samples), err)
	}
	if samples[0].Detail != "HTTP 200" || samples[0].LatencyMS == nil || *samples[0].LatencyMS != 25 {
		t.Fatalf("sample unexpectedly lost latency or status detail: %+v", samples[0])
	}
	if _, err := db.Exec("UPDATE health_samples SET checked_at=1 WHERE check_id=?", check.ID); err != nil {
		t.Fatal(err)
	}
	deleted, err := service.CleanupSamples(t.Context(), time.Unix(2, 0))
	if err != nil || deleted != 5 {
		t.Fatalf("CleanupSamples deleted=%d err=%v want=5", deleted, err)
	}
}

func TestSchedulerRunsDueCheckAndPublishesStateEvent(t *testing.T) {
	db := monitorDB(t)
	projectID := createMonitorProject(t, db)
	service := NewService(db, checkerFunc(func(context.Context, HealthCheck) (ProbeResult, error) {
		return ProbeResult{OK: true, Latency: 12 * time.Millisecond}, nil
	}), 2)
	check, err := service.Create(t.Context(), projectID, CreateInput{Kind: KindTCP, Target: "probe.example:443", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	broker := events.NewBroker(8, 4)
	_, sub := broker.Subscribe(0)
	defer sub.Close()
	scheduler := NewScheduler(service, broker, time.Millisecond)
	if started := scheduler.RunDue(t.Context()); started != 1 {
		t.Fatalf("RunDue() started=%d want=1", started)
	}
	var sawState bool
	deadline := time.After(time.Second)
	for !sawState {
		select {
		case event := <-sub.Events:
			if event.Type == "health.state" {
				sawState = true
			}
		case <-deadline:
			t.Fatal("scheduler did not publish a health state event")
		}
	}
	updated, err := service.Get(t.Context(), check.ID)
	if err != nil || updated.State != StateHealthy {
		t.Fatalf("scheduled check state=%s err=%v want healthy", updated.State, err)
	}
	if started := scheduler.RunDue(t.Context()); started != 0 {
		t.Fatalf("RunDue() reran fresh check %d times", started)
	}
}

func TestAgentCheckFailurePersistsDistinctAgentUnreachableState(t *testing.T) {
	db := monitorDB(t)
	projectID := createMonitorProject(t, db)
	service := NewService(db, &DefaultChecker{}, 1)
	check, err := service.Create(t.Context(), projectID, CreateInput{Kind: KindAgent, Target: "agent-123", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	check, err = service.RunOnce(t.Context(), check.ID)
	if err != nil {
		t.Fatal(err)
	}
	if check.State != StateAgentUnreachable {
		t.Fatalf("state=%s want agent_unreachable", check.State)
	}
	samples, err := service.Samples(t.Context(), check.ID, 10)
	if err != nil || len(samples) != 1 || samples[0].Detail != "agent unreachable" {
		t.Fatalf("agent sample=%+v err=%v", samples, err)
	}
}

func TestCreateRejectsUnsafeOrInvalidProbeTargets(t *testing.T) {
	db := monitorDB(t)
	projectID := createMonitorProject(t, db)
	service := NewService(db, nil, 1)
	for _, input := range []CreateInput{{Kind: KindHTTP, Target: "file:///etc/passwd", Enabled: true}, {Kind: KindTCP, Target: "127.0.0.1:80", IntervalSeconds: 4, Enabled: true}, {Kind: KindAgent, Target: "agent id", Enabled: true}} {
		if _, err := service.Create(t.Context(), projectID, input); !errors.Is(err, ErrInvalidCheck) {
			t.Errorf("Create(%+v) error=%v want ErrInvalidCheck", input, err)
		}
	}
}

func TestRunOnceHonorsConfiguredConcurrencyLimit(t *testing.T) {
	db := monitorDB(t)
	projectID := createMonitorProject(t, db)
	entered := make(chan struct{}, 8)
	release := make(chan struct{}, 8)
	var active, maxActive atomic.Int32
	service := NewService(db, checkerFunc(func(context.Context, HealthCheck) (ProbeResult, error) {
		current := active.Add(1)
		for {
			old := maxActive.Load()
			if current <= old || maxActive.CompareAndSwap(old, current) {
				break
			}
		}
		entered <- struct{}{}
		<-release
		active.Add(-1)
		return ProbeResult{OK: true}, nil
	}), 2)
	checks := make([]HealthCheck, 5)
	for i := range checks {
		check, err := service.Create(t.Context(), projectID, CreateInput{Kind: KindHTTP, Target: "https://monitor.example/check", Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		checks[i] = check
	}
	var wg sync.WaitGroup
	for _, check := range checks {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			if _, err := service.RunOnce(context.Background(), id); err != nil {
				t.Errorf("RunOnce: %v", err)
			}
		}(check.ID)
	}
	for range 2 {
		<-entered
	}
	time.Sleep(20 * time.Millisecond)
	if got := maxActive.Load(); got > 2 {
		t.Fatalf("concurrent probes=%d want <=2", got)
	}
	for i := 0; i < len(checks); i++ {
		release <- struct{}{}
		if i < len(checks)-2 {
			<-entered
		}
	}
	wg.Wait()
	if got := maxActive.Load(); got != 2 {
		t.Fatalf("maximum concurrent probes=%d want 2", got)
	}
}

func monitorDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "monitor.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err = migrate.Apply(t.Context(), db, migrations.FS); err != nil {
		db.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
func createMonitorProject(t *testing.T, db *sql.DB) string {
	t.Helper()
	item, err := projects.NewService(db).Create(t.Context(), projects.CreateInput{Name: fmt.Sprintf("Monitor test project %d", monitorProjectCounter.Add(1))})
	if err != nil {
		t.Fatal(err)
	}
	return item.ID
}
