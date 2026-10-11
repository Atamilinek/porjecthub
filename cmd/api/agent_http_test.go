package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/projecthub/projecthub/internal/agent"
	"github.com/projecthub/projecthub/internal/auth"
	"github.com/projecthub/projecthub/internal/jobs"
	"github.com/projecthub/projecthub/internal/monitor"
	"github.com/projecthub/projecthub/internal/projects"
)

type apiAgentClient struct {
	mu       sync.Mutex
	requests []agent.ActionRequest
	err      error
}

func (c *apiAgentClient) Execute(_ context.Context, request agent.ActionRequest) (agent.Result, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requests = append(c.requests, request)
	if c.err != nil {
		return agent.Result{}, c.err
	}
	return agent.Result{RequestID: request.RequestID, TargetID: request.TargetID, Kind: agent.KindDocker, Action: request.Action, State: agent.StateRunning, CompletedAt: time.Now().Unix()}, nil
}

func TestHostTargetsAndActionsRequireAuthAndUseAllowlistedAgentIdentity(t *testing.T) {
	db := testDatabase(t)
	authService := auth.NewService(db)
	admin, err := authService.BootstrapAdmin(t.Context(), "agent-admin", "correct-horse-battery-staple")
	if err != nil {
		t.Fatal(err)
	}
	session, err := authService.NewSession(t.Context(), admin.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	project, err := projects.NewService(db).Create(t.Context(), projects.CreateInput{Name: "Controlled service"})
	if err != nil {
		t.Fatal(err)
	}
	client := &apiAgentClient{}
	handler, _, dispatcher := newRuntime(db, nil, client)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go dispatcher.Run(ctx)
	<-dispatcher.Ready()
	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/v1/hosts", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous hosts status=%d", unauthorized.Code)
	}
	viewerID := "77777777-7777-4777-8777-777777777777"
	if _, err := db.Exec("INSERT INTO users(id,username,password_hash,role,disabled,created_at,updated_at) VALUES(?,?,?,'viewer',0,1,1)", viewerID, "agent-viewer", "unused"); err != nil {
		t.Fatal(err)
	}
	viewer, err := authService.NewSession(t.Context(), viewerID, "")
	if err != nil {
		t.Fatal(err)
	}
	operatorID := "66666666-6666-4666-8666-666666666666"
	if _, err := db.Exec("INSERT INTO users(id,username,password_hash,role,disabled,created_at,updated_at) VALUES(?,?,?,'operator',0,1,1)", operatorID, "agent-operator", "unused"); err != nil {
		t.Fatal(err)
	}
	operator, err := authService.NewSession(t.Context(), operatorID, "")
	if err != nil {
		t.Fatal(err)
	}
	createdHost := httptest.NewRecorder()
	handler.ServeHTTP(createdHost, healthRequest(http.MethodPost, "/api/v1/hosts", `{"name":"local-agent","agent_id":"agent-local"}`, session))
	if createdHost.Code != http.StatusCreated {
		t.Fatalf("create host status=%d body=%s", createdHost.Code, createdHost.Body.String())
	}
	var host struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(createdHost.Body.Bytes(), &host); err != nil || host.ID == "" {
		t.Fatalf("host=%+v err=%v", host, err)
	}
	targetBody := `{"project_id":"` + project.ID + `","host_id":"` + host.ID + `","agent_target_id":"policy-controlled-service","kind":"docker_container","display_name":"Demo"}`
	createdTarget := httptest.NewRecorder()
	handler.ServeHTTP(createdTarget, healthRequest(http.MethodPost, "/api/v1/service-targets", targetBody, session))
	if createdTarget.Code != http.StatusCreated {
		t.Fatalf("create target status=%d body=%s", createdTarget.Code, createdTarget.Body.String())
	}
	var target struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(createdTarget.Body.Bytes(), &target); err != nil || target.ID == "" {
		t.Fatalf("target=%+v err=%v", target, err)
	}
	statusResponse := httptest.NewRecorder()
	handler.ServeHTTP(statusResponse, healthRequest(http.MethodGet, "/api/v1/service-targets/"+target.ID+"/status", "", session))
	if statusResponse.Code != http.StatusOK || !strings.Contains(statusResponse.Body.String(), `"state":"running"`) {
		t.Fatalf("target status=%d body=%s", statusResponse.Code, statusResponse.Body.String())
	}
	denied := httptest.NewRecorder()
	viewerAction := healthRequest(http.MethodPost, "/api/v1/service-targets/"+target.ID+"/actions", `{"action":"restart"}`, viewer)
	viewerAction.Header.Set("Idempotency-Key", "88888888-8888-4888-8888-888888888888")
	handler.ServeHTTP(denied, viewerAction)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("viewer action status=%d", denied.Code)
	}
	operatorDenied := httptest.NewRecorder()
	operatorAction := healthRequest(http.MethodPost, "/api/v1/service-targets/"+target.ID+"/actions", `{"action":"restart"}`, operator)
	operatorAction.Header.Set("Idempotency-Key", "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	handler.ServeHTTP(operatorDenied, operatorAction)
	if operatorDenied.Code != http.StatusForbidden {
		t.Fatalf("operator action status=%d body=%s", operatorDenied.Code, operatorDenied.Body.String())
	}
	key := "99999999-9999-4999-8999-999999999999"
	action := healthRequest(http.MethodPost, "/api/v1/service-targets/"+target.ID+"/actions", `{"action":"restart"}`, session)
	action.Header.Set("Idempotency-Key", key)
	accepted := httptest.NewRecorder()
	handler.ServeHTTP(accepted, action)
	if accepted.Code != http.StatusAccepted {
		t.Fatalf("action status=%d body=%s", accepted.Code, accepted.Body.String())
	}
	var queued jobs.Job
	if err := json.Unmarshal(accepted.Body.Bytes(), &queued); err != nil || queued.ID == "" {
		t.Fatalf("job=%+v err=%v", queued, err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		job, err := dispatcher.Get(t.Context(), queued.ID)
		if err == nil && job.Status == jobs.StatusSucceeded {
			queued = job
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if queued.Status != jobs.StatusSucceeded {
		t.Fatalf("job=%+v", queued)
	}
	replay := healthRequest(http.MethodPost, "/api/v1/service-targets/"+target.ID+"/actions", `{"action":"restart"}`, session)
	replay.Header.Set("Idempotency-Key", key)
	replayed := httptest.NewRecorder()
	handler.ServeHTTP(replayed, replay)
	if replayed.Code != http.StatusOK {
		t.Fatalf("idempotent replay status=%d body=%s", replayed.Code, replayed.Body.String())
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.requests) != 2 || client.requests[0].AgentID != "agent-local" || client.requests[0].TargetID != "policy-controlled-service" || client.requests[0].Action != agent.ActionStatus || client.requests[1].Action != agent.ActionRestart {
		t.Fatalf("Agent requests=%+v", client.requests)
	}
	var auditCount int
	if err := db.QueryRow("SELECT count(*) FROM audit_logs WHERE actor_id=? AND action='service.restart' AND outcome='succeeded'", admin.ID).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatalf("completion audit count=%d err=%v", auditCount, err)
	}
}

func TestActionDispatcherMarksMissingAgentUnavailable(t *testing.T) {
	db := testDatabase(t)
	seedAgentAPIRecords(t, db)
	dispatcher := jobs.NewDispatcher(db, nil, nil, 1, 4)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go dispatcher.Run(ctx)
	<-dispatcher.Ready()
	job, _, err := dispatcher.Create(t.Context(), jobs.CreateInput{ActorID: "11111111-1111-4111-8111-111111111111", TargetID: "44444444-4444-4444-8444-444444444444", Action: agent.ActionStart, IdempotencyKey: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		job, _ = dispatcher.Get(t.Context(), job.ID)
		if job.Status == jobs.StatusFailed {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if job.Status != jobs.StatusFailed || job.ErrorCode != "agent_unavailable" {
		t.Fatalf("job=%+v", job)
	}
}

func TestMonitorAgentProbeUsesActualAgentStatusAndSeparatesUnavailable(t *testing.T) {
	db := testDatabase(t)
	seedAgentAPIRecords(t, db)
	client := &apiAgentClient{}
	dispatcher := jobs.NewDispatcher(db, client, nil, 1, 4)
	probe := monitorAgentProbe{dispatcher: dispatcher}
	result, err := probe.CheckAgent(t.Context(), "44444444-4444-4444-8444-444444444444")
	if err != nil || !result.OK {
		t.Fatalf("running probe=%+v err=%v", result, err)
	}
	missing := monitorAgentProbe{dispatcher: jobs.NewDispatcher(db, nil, nil, 1, 4)}
	_, err = missing.CheckAgent(t.Context(), "44444444-4444-4444-8444-444444444444")
	if !errors.Is(err, monitor.ErrAgentUnavailable) {
		t.Fatalf("missing agent probe error=%v", err)
	}
}

func seedAgentAPIRecords(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO users(id,username,password_hash,role,created_at,updated_at) VALUES('11111111-1111-4111-8111-111111111111','admin','unused','admin',1,1);INSERT INTO projects(id,name,created_at,updated_at) VALUES('22222222-2222-4222-8222-222222222222','Agent API project',1,1);INSERT INTO hosts(id,name,agent_id,created_at,updated_at) VALUES('33333333-3333-4333-8333-333333333333','Agent host','agent-west',1,1);INSERT INTO service_targets(id,project_id,host_id,agent_target_id,kind,display_name,created_at) VALUES('44444444-4444-4444-8444-444444444444','22222222-2222-4222-8222-222222222222','33333333-3333-4333-8333-333333333333','policy-app','docker_container','Demo',1);`)
	if err != nil {
		t.Fatal(err)
	}
}
