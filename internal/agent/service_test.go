package agent

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeBackend struct {
	mu                                          sync.Mutex
	state                                       State
	statusCalls                                 int
	doCalls                                     int
	delay                                       time.Duration
	statusErr                                   error
	doErr                                       error
	transitionDelay                             time.Duration
	activeTransitions, maxConcurrentTransitions int
}

func (f *fakeBackend) Status(ctx context.Context, _ Target) (State, error) {
	f.mu.Lock()
	f.statusCalls++
	err := f.statusErr
	state := f.state
	f.mu.Unlock()
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return state, err
}
func (f *fakeBackend) Transition(_ context.Context, _ Target, action Action) error {
	f.mu.Lock()
	f.doCalls++
	f.activeTransitions++
	if f.activeTransitions > f.maxConcurrentTransitions {
		f.maxConcurrentTransitions = f.activeTransitions
	}
	err := f.doErr
	delay := f.transitionDelay
	f.mu.Unlock()
	if delay > 0 {
		time.Sleep(delay)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.activeTransitions--
	if err != nil {
		return err
	}
	switch action {
	case ActionStart:
		f.state = StateRunning
	case ActionStop:
		f.state = StateStopped
	case ActionRestart:
		f.state = StateRunning
	}
	return nil
}

func validRequest(requestID, targetID string, action Action) ActionRequest {
	return ActionRequest{RequestID: requestID, AgentID: "agent-1", TargetID: targetID, Action: action}
}

func TestServiceDeniesBeforeCallingBackend(t *testing.T) {
	protected := Target{ID: "core-api", Kind: KindSystemd, Identity: "projecthub-api.service", Actions: []Action{ActionStatus, ActionRestart}, Protected: true}
	backend := &fakeBackend{state: StateRunning}
	service := NewService(NewPolicy("agent-1", []Target{protected}), backend, time.Second)
	_, err := service.Execute(context.Background(), validRequest("request-1", protected.ID, ActionRestart))
	if !errors.Is(err, ErrProtectedTarget) {
		t.Fatalf("protected target error=%v", err)
	}
	if backend.statusCalls != 0 || backend.doCalls != 0 {
		t.Fatalf("backend called on policy denial: status=%d transition=%d", backend.statusCalls, backend.doCalls)
	}
	_, err = service.Execute(context.Background(), validRequest("request-2", "missing", ActionStop))
	if !errors.Is(err, ErrTargetNotFound) {
		t.Fatalf("missing target error=%v", err)
	}
	_, err = service.Execute(context.Background(), validRequest("request-3", protected.ID, Action("shell")))
	if !errors.Is(err, ErrUnsupportedAction) {
		t.Fatalf("unsupported action error=%v", err)
	}
	if backend.statusCalls != 0 || backend.doCalls != 0 {
		t.Fatal("backend called after invalid request")
	}
}

func TestHardProtectedControlPlaneUnitIsDeniedEvenIfPolicyFlagIsFalse(t *testing.T) {
	target := Target{ID: "api-unit", Kind: KindSystemd, Identity: "projecthub-api.service", Actions: []Action{ActionStatus, ActionStop}}
	backend := &fakeBackend{state: StateRunning}
	service := NewService(NewPolicy("agent-1", []Target{target}), backend, time.Second)
	_, err := service.Execute(context.Background(), validRequest("protect-api", "api-unit", ActionStop))
	if !errors.Is(err, ErrProtectedTarget) || backend.statusCalls != 0 || backend.doCalls != 0 {
		t.Fatalf("protected unit err=%v calls=(%d,%d)", err, backend.statusCalls, backend.doCalls)
	}
}

func TestServiceChecksIdentityAndReadsBackAfterAction(t *testing.T) {
	target := Target{ID: "app-container", Kind: KindDocker, Identity: "sha256:expected", Actions: []Action{ActionStatus, ActionStart, ActionStop, ActionRestart}}
	backend := &fakeBackend{state: StateStopped}
	service := NewService(NewPolicy("agent-1", []Target{target}), backend, time.Second)
	result, err := service.Execute(context.Background(), validRequest("request-start", target.ID, ActionStart))
	if err != nil || result.State != StateRunning || backend.statusCalls != 2 || backend.doCalls != 1 {
		t.Fatalf("result=%+v err=%v calls=(%d,%d)", result, err, backend.statusCalls, backend.doCalls)
	}
	backend.statusErr = ErrIdentityMismatch
	_, err = service.Execute(context.Background(), validRequest("request-stop", target.ID, ActionStop))
	if !errors.Is(err, ErrIdentityMismatch) || backend.doCalls != 1 {
		t.Fatalf("identity mismatch must deny transition: err=%v doCalls=%d", err, backend.doCalls)
	}
}

func TestServiceIsIdempotentAndSerializesSameTarget(t *testing.T) {
	target := Target{ID: "app", Kind: KindDocker, Identity: "sha256:app", Actions: []Action{ActionStatus, ActionStart, ActionStop, ActionRestart}}
	backend := &fakeBackend{state: StateStopped, delay: 15 * time.Millisecond}
	service := NewService(NewPolicy("agent-1", []Target{target}), backend, time.Second)
	request := validRequest("same-request", target.ID, ActionStart)
	first, err := service.Execute(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Execute(context.Background(), request)
	if err != nil || first != second || backend.doCalls != 1 {
		t.Fatalf("idempotent replay first=%+v second=%+v err=%v calls=%d", first, second, err, backend.doCalls)
	}
	_, err = service.Execute(context.Background(), validRequest("same-request", target.ID, ActionStop))
	if !errors.Is(err, ErrRequestConflict) {
		t.Fatalf("request ID reuse error=%v", err)
	}
	backend.delay = 0
	backend.transitionDelay = 20 * time.Millisecond
	request1 := validRequest("parallel-1", target.ID, ActionRestart)
	request2 := validRequest("parallel-2", target.ID, ActionStop)
	var wg sync.WaitGroup
	for _, item := range []ActionRequest{request1, request2} {
		wg.Add(1)
		go func(req ActionRequest) { defer wg.Done(); _, _ = service.Execute(context.Background(), req) }(item)
	}
	wg.Wait()
	if backend.doCalls != 3 || backend.maxConcurrentTransitions != 1 {
		t.Fatalf("transition calls=%d max concurrency=%d, want serialized requests", backend.doCalls, backend.maxConcurrentTransitions)
	}
}

func TestServiceHonorsDeadlineAndAgentPolicyIdentity(t *testing.T) {
	service := NewService(NewPolicy("agent-1", []Target{{ID: "app", Kind: KindDocker, Identity: "sha256:app", Actions: []Action{ActionRestart}}}), &fakeBackend{state: StateRunning, delay: 30 * time.Millisecond}, 5*time.Millisecond)
	_, err := service.Execute(context.Background(), validRequest("timeout", "app", ActionRestart))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline error=%v", err)
	}
	_, err = service.Execute(context.Background(), ActionRequest{RequestID: "bad-agent", AgentID: "other-agent", TargetID: "app", Action: ActionRestart})
	if !errors.Is(err, ErrAgentIdentity) {
		t.Fatalf("agent identity error=%v", err)
	}
}
