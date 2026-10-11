package jobs

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/projecthub/projecthub/internal/agent"
	"github.com/projecthub/projecthub/internal/migrate"
	"github.com/projecthub/projecthub/internal/store"
	"github.com/projecthub/projecthub/migrations"
)

type fakeAgentClient struct {
	mu       sync.Mutex
	requests []agent.ActionRequest
	result   agent.Result
	err      error
}

func (f *fakeAgentClient) Execute(_ context.Context, request agent.ActionRequest) (agent.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, request)
	return f.result, f.err
}

func TestDispatcherUsesHostAllowlistMappingAndIdempotency(t *testing.T) {
	db := testDB(t)
	seedTarget(t, db)
	client := &fakeAgentClient{result: agent.Result{Kind: agent.KindDocker, State: agent.StateRunning}}
	dispatcher := NewDispatcher(db, client, nil, 2, 8)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go dispatcher.Run(ctx)
	<-dispatcher.ready
	input := CreateInput{ActorID: "11111111-1111-4111-8111-111111111111", TargetID: "44444444-4444-4444-8444-444444444444", Action: agent.ActionRestart, IdempotencyKey: "55555555-5555-4555-8555-555555555555"}
	created, fresh, err := dispatcher.Create(context.Background(), input)
	if err != nil || !fresh || created.Status != StatusQueued {
		t.Fatalf("create=%+v fresh=%v err=%v", created, fresh, err)
	}
	var queuedAudit int
	if err := db.QueryRow("SELECT count(*) FROM audit_logs WHERE request_id=? AND outcome='queued'", created.RequestID).Scan(&queuedAudit); err != nil || queuedAudit != 1 {
		t.Fatalf("queued audit count=%d err=%v", queuedAudit, err)
	}
	replay, fresh, err := dispatcher.Create(context.Background(), input)
	if err != nil || fresh || replay.ID != created.ID {
		t.Fatalf("idempotent replay=%+v fresh=%v err=%v", replay, fresh, err)
	}
	input.Action = agent.ActionStop
	if _, _, err := dispatcher.Create(context.Background(), input); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("idempotency key conflict=%v", err)
	}
	input.Action = agent.ActionRestart
	input.IdempotencyKey = "66666666-6666-4666-8666-666666666666"
	job, fresh, err := dispatcher.Create(context.Background(), input)
	if err != nil || !fresh {
		t.Fatalf("submit=%+v fresh=%v err=%v", job, fresh, err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		job, err = dispatcher.Get(context.Background(), job.ID)
		if err == nil && job.Status == StatusSucceeded {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if job.Status != StatusSucceeded {
		t.Fatalf("job did not complete: %+v", job)
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.requests) != 2 || client.requests[0].AgentID != "agent-west" || client.requests[0].TargetID != "policy-app" || client.requests[0].Action != agent.ActionRestart || client.requests[1].Action != agent.ActionRestart {
		t.Fatalf("Agent request=%+v", client.requests)
	}
}

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "jobs.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := migrate.Apply(context.Background(), db, migrations.FS); err != nil {
		db.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
func seedTarget(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO users(id,username,password_hash,role,created_at,updated_at) VALUES('11111111-1111-4111-8111-111111111111','admin','unused','admin',1,1);
INSERT INTO projects(id,name,created_at,updated_at) VALUES('22222222-2222-4222-8222-222222222222','Jobs project',1,1);
INSERT INTO hosts(id,name,agent_id,created_at,updated_at) VALUES('33333333-3333-4333-8333-333333333333','Local agent','agent-west',1,1);
INSERT INTO service_targets(id,project_id,host_id,agent_target_id,kind,display_name,created_at) VALUES('44444444-4444-4444-8444-444444444444','22222222-2222-4222-8222-222222222222','33333333-3333-4333-8333-333333333333','policy-app','docker_container','Demo service',1);`)
	if err != nil {
		t.Fatal(err)
	}
}
