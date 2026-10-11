package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/projecthub/projecthub/internal/agent"
	"github.com/projecthub/projecthub/internal/events"
	"github.com/projecthub/projecthub/internal/ids"
)

type Status string

const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusTimedOut  Status = "timed_out"
)

var (
	ErrInvalidAction         = errors.New("invalid service action")
	ErrTargetNotFound        = errors.New("service target not found")
	ErrIdempotencyConflict   = errors.New("idempotency key belongs to a different action")
	ErrQueueFull             = errors.New("action queue is full")
	ErrJobNotFound           = errors.New("action job not found")
	ErrAuditUnavailable      = errors.New("action audit unavailable")
	ErrDispatcherUnavailable = errors.New("action dispatcher unavailable")
)

type Job struct {
	ID          string       `json:"id"`
	RequestID   string       `json:"request_id"`
	ActorID     string       `json:"actor_id"`
	ProjectID   string       `json:"project_id"`
	TargetID    string       `json:"target_id"`
	Action      agent.Action `json:"action"`
	Status      Status       `json:"status"`
	Result      string       `json:"result"`
	ErrorCode   string       `json:"error_code"`
	CreatedAt   int64        `json:"created_at"`
	StartedAt   *int64       `json:"started_at,omitempty"`
	CompletedAt *int64       `json:"completed_at,omitempty"`
}
type CreateInput struct {
	ActorID, TargetID string
	Action            agent.Action
	IdempotencyKey    string
}
type dispatchItem struct {
	job                    Job
	agentID, agentTargetID string
	targetKind             agent.Kind
}
type actionClient interface {
	Execute(context.Context, agent.ActionRequest) (agent.Result, error)
}
type Dispatcher struct {
	db          *sql.DB
	client      actionClient
	broker      *events.Broker
	workers     int
	queue       chan dispatchItem
	statusSlots chan struct{}
	ready       chan struct{}
	readyOnce   sync.Once
	startupErr  error
	wg          sync.WaitGroup
}

func NewDispatcher(db *sql.DB, client actionClient, broker *events.Broker, workers, queueSize int) *Dispatcher {
	if workers < 1 {
		workers = 2
	}
	if queueSize < workers {
		queueSize = workers
	}
	return &Dispatcher{db: db, client: client, broker: broker, workers: workers, queue: make(chan dispatchItem, queueSize), statusSlots: make(chan struct{}, workers), ready: make(chan struct{})}
}
func (d *Dispatcher) Ready() <-chan struct{} { return d.ready }
func (d *Dispatcher) Status(ctx context.Context, targetID string) (agent.Result, error) {
	if !ids.IsUUID(targetID) {
		return agent.Result{}, ErrTargetNotFound
	}
	var agentID, agentTargetID, hostID string
	var targetKind agent.Kind
	err := d.db.QueryRowContext(ctx, `SELECT h.agent_id,t.agent_target_id,h.id,t.kind FROM service_targets t JOIN hosts h ON h.id=t.host_id WHERE t.id=?`, targetID).Scan(&agentID, &agentTargetID, &hostID, &targetKind)
	if errors.Is(err, sql.ErrNoRows) {
		return agent.Result{}, ErrTargetNotFound
	}
	if err != nil {
		return agent.Result{}, err
	}
	if d.client == nil {
		return agent.Result{}, agent.ErrAgentUnavailable
	}
	select {
	case d.statusSlots <- struct{}{}:
		defer func() { <-d.statusSlots }()
	case <-ctx.Done():
		return agent.Result{}, ctx.Err()
	}
	requestID, err := ids.NewUUID()
	if err != nil {
		return agent.Result{}, err
	}
	callCtx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	result, err := d.client.Execute(callCtx, agent.ActionRequest{RequestID: requestID, AgentID: agentID, TargetID: agentTargetID, Action: agent.ActionStatus})
	if err != nil {
		return agent.Result{}, err
	}
	if agent.Kind(targetKind) != result.Kind {
		return agent.Result{}, agent.ErrTargetKindMismatch
	}
	_, _ = d.db.ExecContext(ctx, "UPDATE hosts SET last_seen_at=?,updated_at=? WHERE id=?", time.Now().Unix(), time.Now().Unix(), hostID)
	return result, nil
}
func (d *Dispatcher) Run(ctx context.Context) error {
	if _, err := d.db.ExecContext(ctx, "UPDATE action_jobs SET status='failed',error_code='interrupted',completed_at=? WHERE status IN ('queued','running')", time.Now().Unix()); err != nil {
		d.startupErr = err
		d.readyOnce.Do(func() { close(d.ready) })
		return err
	}
	for i := 0; i < d.workers; i++ {
		d.wg.Add(1)
		go d.worker(ctx)
	}
	d.readyOnce.Do(func() { close(d.ready) })
	<-ctx.Done()
	d.wg.Wait()
	return ctx.Err()
}
func (d *Dispatcher) Create(ctx context.Context, input CreateInput) (Job, bool, error) {
	if input.Action != agent.ActionStart && input.Action != agent.ActionStop && input.Action != agent.ActionRestart || !ids.IsUUID(input.TargetID) || !ids.IsUUID(input.IdempotencyKey) {
		return Job{}, false, ErrInvalidAction
	}
	if input.ActorID != "" && !ids.IsUUID(input.ActorID) {
		return Job{}, false, ErrInvalidAction
	}
	select {
	case <-d.ready:
	case <-ctx.Done():
		return Job{}, false, ctx.Err()
	}
	if d.startupErr != nil {
		return Job{}, false, ErrDispatcherUnavailable
	}
	if job, err := d.byIdempotency(ctx, input.IdempotencyKey); err == nil {
		if job.TargetID != input.TargetID || job.Action != input.Action || job.ActorID != input.ActorID {
			return Job{}, false, ErrIdempotencyConflict
		}
		return job, false, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Job{}, false, err
	}
	var projectID, agentID, agentTargetID string
	var targetKind agent.Kind
	err := d.db.QueryRowContext(ctx, `SELECT t.project_id,h.agent_id,t.agent_target_id,t.kind FROM service_targets t JOIN hosts h ON h.id=t.host_id WHERE t.id=?`, input.TargetID).Scan(&projectID, &agentID, &agentTargetID, &targetKind)
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, false, ErrTargetNotFound
	}
	if err != nil {
		return Job{}, false, err
	}
	jobID, err := ids.NewUUID()
	if err != nil {
		return Job{}, false, err
	}
	requestID, err := ids.NewUUID()
	if err != nil {
		return Job{}, false, err
	}
	now := time.Now().Unix()
	_, err = d.db.ExecContext(ctx, `INSERT INTO action_jobs(id,request_id,idempotency_key,actor_id,project_id,target_id,action,status,result,error_code,created_at) VALUES(?,?,?,?,?,?,?,'queued','','',?)`, jobID, requestID, input.IdempotencyKey, nullable(input.ActorID), projectID, input.TargetID, input.Action, now)
	if err != nil {
		if job, lookupErr := d.byIdempotency(ctx, input.IdempotencyKey); lookupErr == nil {
			if job.TargetID != input.TargetID || job.Action != input.Action || job.ActorID != input.ActorID {
				return Job{}, false, ErrIdempotencyConflict
			}
			return job, false, nil
		}
		return Job{}, false, err
	}
	job, err := d.Get(ctx, jobID)
	if err != nil {
		return Job{}, false, err
	}
	if err := d.recordAudit(ctx, job, "queued", ""); err != nil {
		_, _ = d.db.ExecContext(ctx, "UPDATE action_jobs SET status='failed',error_code='audit_unavailable',completed_at=? WHERE id=? AND status='queued'", time.Now().Unix(), job.ID)
		return Job{}, false, ErrAuditUnavailable
	}
	item := dispatchItem{job: job, agentID: agentID, agentTargetID: agentTargetID, targetKind: targetKind}
	select {
	case d.queue <- item:
		d.publish(job)
	default:
		_, _ = d.db.ExecContext(ctx, "UPDATE action_jobs SET status='failed',error_code='queue_full',completed_at=? WHERE id=? AND status='queued'", time.Now().Unix(), job.ID)
		_ = d.recordAudit(ctx, job, "failed", "queue_full")
		job, _ = d.Get(ctx, job.ID)
		return job, true, ErrQueueFull
	}
	return job, true, nil
}
func (d *Dispatcher) Get(ctx context.Context, id string) (Job, error) {
	var job Job
	var actor sql.NullString
	var started, completed sql.NullInt64
	err := d.db.QueryRowContext(ctx, `SELECT id,request_id,actor_id,project_id,target_id,action,status,result,error_code,created_at,started_at,completed_at FROM action_jobs WHERE id=?`, id).Scan(&job.ID, &job.RequestID, &actor, &job.ProjectID, &job.TargetID, &job.Action, &job.Status, &job.Result, &job.ErrorCode, &job.CreatedAt, &started, &completed)
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, ErrJobNotFound
	}
	if err != nil {
		return Job{}, err
	}
	if actor.Valid {
		job.ActorID = actor.String
	}
	if started.Valid {
		job.StartedAt = &started.Int64
	}
	if completed.Valid {
		job.CompletedAt = &completed.Int64
	}
	return job, nil
}
func (d *Dispatcher) List(ctx context.Context, limit int) ([]Job, error) {
	if limit < 1 || limit > 200 {
		limit = 100
	}
	rows, err := d.db.QueryContext(ctx, "SELECT id FROM action_jobs ORDER BY created_at DESC,id DESC LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Job, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		job, err := d.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		items = append(items, job)
	}
	return items, rows.Err()
}
func (d *Dispatcher) byIdempotency(ctx context.Context, key string) (Job, error) {
	var id string
	err := d.db.QueryRowContext(ctx, "SELECT id FROM action_jobs WHERE idempotency_key=?", key).Scan(&id)
	if err != nil {
		return Job{}, err
	}
	return d.Get(ctx, id)
}
func (d *Dispatcher) worker(ctx context.Context) {
	defer d.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case item := <-d.queue:
			d.process(ctx, item)
		}
	}
}
func (d *Dispatcher) process(parent context.Context, item dispatchItem) {
	now := time.Now().Unix()
	res, err := d.db.ExecContext(parent, "UPDATE action_jobs SET status='running',started_at=? WHERE id=? AND status='queued'", now, item.job.ID)
	if err != nil {
		return
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return
	}
	item.job.Status = StatusRunning
	item.job.StartedAt = &now
	d.publish(item.job)
	ctx, cancel := context.WithTimeout(parent, 35*time.Second)
	defer cancel()
	var result agent.Result
	if d.client == nil {
		err = agent.ErrAgentUnavailable
	} else {
		result, err = d.client.Execute(ctx, agent.ActionRequest{RequestID: item.job.RequestID, AgentID: item.agentID, TargetID: item.agentTargetID, Action: item.job.Action})
		if err == nil && result.Kind != item.targetKind {
			err = agent.ErrTargetKindMismatch
		}
	}
	status, code, detail := StatusSucceeded, "", result.State
	if err != nil {
		status, code, detail = StatusFailed, "backend_failed", "action failed"
		if errors.Is(err, context.DeadlineExceeded) {
			status, code = StatusTimedOut, "timeout"
		} else if errors.Is(err, agent.ErrTargetKindMismatch) {
			code = "target_kind_mismatch"
		} else if errors.Is(err, agent.ErrAgentUnavailable) {
			code = "agent_unavailable"
		} else {
			var remote *agent.RemoteError
			if errors.As(err, &remote) {
				code = remote.Code
				if code == "" {
					code = "agent_rejected"
				}
			}
		}
	}
	completed := time.Now().Unix()
	resultJSON, _ := json.Marshal(map[string]string{"state": string(detail)})
	_, _ = d.db.ExecContext(parent, "UPDATE action_jobs SET status=?,result=?,error_code=?,completed_at=? WHERE id=? AND status='running'", status, string(resultJSON), code, completed, item.job.ID)
	_ = d.recordAudit(parent, item.job, string(status), code)
	item.job.Status = status
	item.job.Result = string(resultJSON)
	item.job.ErrorCode = code
	item.job.CompletedAt = &completed
	d.publish(item.job)
}
func (d *Dispatcher) recordAudit(ctx context.Context, job Job, outcome, detail string) error {
	auditID, err := ids.NewUUID()
	if err != nil {
		return err
	}
	_, err = d.db.ExecContext(ctx, "INSERT INTO audit_logs(id,actor_id,request_id,event_type,target_type,target_id,action,outcome,detail,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)", auditID, nullable(job.ActorID), job.RequestID, "action", "service_target", job.TargetID, "service."+string(job.Action), outcome, detail, time.Now().Unix())
	return err
}
func (d *Dispatcher) publish(job Job) {
	if d.broker == nil {
		return
	}
	data, _ := json.Marshal(job)
	d.broker.Publish("job.state", data)
}
func nullable(v string) any {
	if v == "" {
		return nil
	}
	return v
}
