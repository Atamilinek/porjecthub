package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

type ActionRequest struct {
	RequestID string `json:"request_id"`
	AgentID   string `json:"agent_id"`
	TargetID  string `json:"target_id"`
	Action    Action `json:"action"`
}
type Result struct {
	RequestID   string `json:"request_id"`
	TargetID    string `json:"target_id"`
	Kind        Kind   `json:"kind"`
	Action      Action `json:"action"`
	State       State  `json:"state"`
	CompletedAt int64  `json:"completed_at"`
}
type Backend interface {
	Status(context.Context, Target) (State, error)
	Transition(context.Context, Target, Action) error
}
type cachedRequest struct {
	signature string
	result    Result
	err       error
}
type inflightRequest struct {
	signature string
	done      chan struct{}
	result    Result
	err       error
}
type Service struct {
	policy      *Policy
	backend     Backend
	timeout     time.Duration
	mu          sync.Mutex
	locks       map[string]chan struct{}
	concurrency chan struct{}
	inflight    map[string]*inflightRequest
	completed   map[string]cachedRequest
	order       []string
}

func NewService(policy *Policy, backend Backend, timeout time.Duration) *Service {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Service{policy: policy, backend: backend, timeout: timeout, locks: make(map[string]chan struct{}), concurrency: make(chan struct{}, 8), inflight: make(map[string]*inflightRequest), completed: make(map[string]cachedRequest)}
}

func (s *Service) Execute(ctx context.Context, request ActionRequest) (Result, error) {
	if request.RequestID == "" || len(request.RequestID) > 128 || request.TargetID == "" || len(request.TargetID) > 128 {
		return Result{}, ErrInvalidPolicy
	}
	if s.policy == nil || request.AgentID != s.policy.AgentID() {
		return Result{}, ErrAgentIdentity
	}
	target, exists := s.policy.Target(request.TargetID)
	if !exists {
		return Result{}, ErrTargetNotFound
	}
	if request.Action != ActionStatus && request.Action != ActionStart && request.Action != ActionStop && request.Action != ActionRestart {
		return Result{}, ErrUnsupportedAction
	}
	if (target.Protected || hardProtectedSystemd(target)) && request.Action != ActionStatus {
		return Result{}, ErrProtectedTarget
	}
	allowed := false
	for _, action := range target.Actions {
		if action == request.Action {
			allowed = true
			break
		}
	}
	if !allowed {
		return Result{}, ErrUnsupportedAction
	}
	requestSig := signature(request)

	s.mu.Lock()
	if cached, ok := s.completed[request.RequestID]; ok {
		s.mu.Unlock()
		if cached.signature != requestSig {
			return Result{}, ErrRequestConflict
		}
		return cached.result, cached.err
	}
	if pending, ok := s.inflight[request.RequestID]; ok {
		if pending.signature != requestSig {
			s.mu.Unlock()
			return Result{}, ErrRequestConflict
		}
		done := pending.done
		s.mu.Unlock()
		select {
		case <-done:
			return pending.result, pending.err
		case <-ctx.Done():
			return Result{}, ctx.Err()
		}
	}
	pending := &inflightRequest{signature: requestSig, done: make(chan struct{})}
	s.inflight[request.RequestID] = pending
	s.mu.Unlock()

	result, err := s.execute(ctx, request, target)
	s.mu.Lock()
	pending.result, pending.err = result, err
	delete(s.inflight, request.RequestID)
	s.completed[request.RequestID] = cachedRequest{signature: requestSig, result: result, err: err}
	s.order = append(s.order, request.RequestID)
	if len(s.order) > 4096 {
		old := s.order[0]
		s.order = s.order[1:]
		delete(s.completed, old)
	}
	close(pending.done)
	s.mu.Unlock()
	return result, err
}

func (s *Service) execute(parent context.Context, request ActionRequest, target Target) (Result, error) {
	ctx, cancel := context.WithTimeout(parent, s.timeout)
	defer cancel()
	select {
	case s.concurrency <- struct{}{}:
		defer func() { <-s.concurrency }()
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
	lock := s.targetLock(target.ID)
	select {
	case <-lock:
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
	defer func() { lock <- struct{}{} }()
	if s.backend == nil {
		return Result{}, errors.New("agent backend unavailable")
	}
	state, err := s.backend.Status(ctx, target)
	if err != nil {
		return Result{}, err
	}
	if request.Action != ActionStatus {
		desired := desiredState(request.Action)
		if request.Action == ActionRestart || state != desired {
			if err := s.backend.Transition(ctx, target, request.Action); err != nil {
				return Result{}, err
			}
			state, err = s.backend.Status(ctx, target)
			if err != nil {
				return Result{}, err
			}
			if state != desired {
				return Result{}, fmt.Errorf("%w: got %s, want %s", ErrStateMismatch, state, desired)
			}
		}
	}
	return Result{RequestID: request.RequestID, TargetID: request.TargetID, Kind: target.Kind, Action: request.Action, State: state, CompletedAt: time.Now().Unix()}, nil
}

func (s *Service) targetLock(id string) chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	lock := s.locks[id]
	if lock == nil {
		lock = make(chan struct{}, 1)
		lock <- struct{}{}
		s.locks[id] = lock
	}
	return lock
}
func desiredState(action Action) State {
	if action == ActionStop {
		return StateStopped
	}
	if action == ActionStart || action == ActionRestart {
		return StateRunning
	}
	return StateUnknown
}
func signature(request ActionRequest) string {
	data := []byte(request.AgentID + "\x00" + request.TargetID + "\x00" + string(request.Action))
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}
