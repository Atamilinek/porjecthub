package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/coreos/go-systemd/v22/dbus"
	containertypes "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

type fakeDocker struct {
	id                                                string
	name                                              string
	labels                                            map[string]string
	stopErr                                           error
	running                                           bool
	inspectCalls, startCalls, stopCalls, restartCalls int
}

func (f *fakeDocker) ContainerInspect(context.Context, string, client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
	f.inspectCalls++
	return client.ContainerInspectResult{Container: containertypes.InspectResponse{ID: f.id, Name: f.name, Config: &containertypes.Config{Labels: f.labels}, State: &containertypes.State{Running: f.running}}}, nil
}
func (f *fakeDocker) ContainerStart(context.Context, string, client.ContainerStartOptions) (client.ContainerStartResult, error) {
	f.startCalls++
	f.running = true
	return client.ContainerStartResult{}, nil
}
func (f *fakeDocker) ContainerStop(context.Context, string, client.ContainerStopOptions) (client.ContainerStopResult, error) {
	f.stopCalls++
	f.running = false
	return client.ContainerStopResult{}, f.stopErr
}
func (f *fakeDocker) ContainerRestart(context.Context, string, client.ContainerRestartOptions) (client.ContainerRestartResult, error) {
	f.restartCalls++
	f.running = true
	return client.ContainerRestartResult{}, nil
}

func TestDockerBackendUsesOnlyExistingIdentityCheckedOperations(t *testing.T) {
	expected := strings.Repeat("a", 64)
	fake := &fakeDocker{id: expected, running: true}
	backend := NewDockerBackend(fake)
	target := Target{Kind: KindDocker, Identity: expected}
	state, err := backend.Status(context.Background(), target)
	if err != nil || state != StateRunning {
		t.Fatalf("status=%s err=%v", state, err)
	}
	if err := backend.Transition(context.Background(), target, ActionRestart); err != nil {
		t.Fatal(err)
	}
	if fake.inspectCalls != 2 || fake.restartCalls != 1 || fake.startCalls+fake.stopCalls != 0 {
		t.Fatalf("fake Docker calls=%+v", fake)
	}
	fake.id = strings.Repeat("b", 64)
	if err := backend.Transition(context.Background(), target, ActionStop); !errors.Is(err, ErrIdentityMismatch) {
		t.Fatalf("changed container identity error=%v", err)
	}
	if fake.stopCalls != 0 {
		t.Fatal("transition called after identity mismatch")
	}
	fake.id = expected
	fake.name = "/projecthub-api"
	target.Identity = expected
	if err := backend.Transition(context.Background(), target, ActionStop); !errors.Is(err, ErrProtectedTarget) || fake.stopCalls != 0 {
		t.Fatalf("protected container error=%v transitions=%d", err, fake.stopCalls)
	}
	fake.name = "/ordinary"
	fake.labels = map[string]string{"com.projecthub.protected": "true"}
	if err := backend.Transition(context.Background(), target, ActionStop); !errors.Is(err, ErrProtectedTarget) || fake.stopCalls != 0 {
		t.Fatalf("protected label error=%v transitions=%d", err, fake.stopCalls)
	}
	fake.labels = nil
	fake.stopErr = errors.New("fake Docker engine unavailable")
	if err := backend.Transition(context.Background(), target, ActionStop); err == nil || fake.stopCalls != 1 {
		t.Fatalf("Docker failure err=%v stop calls=%d", err, fake.stopCalls)
	}
}

func TestDockerClientRequiresExplicitEndpoint(t *testing.T) {
	if _, err := NewDockerClient(""); err == nil {
		t.Fatal("Docker client silently selected a host endpoint")
	}
	for _, endpoint := range []string{"tcp://127.0.0.1:2375", "ssh://host/var/run/docker.sock", "unix://relative.sock"} {
		if _, err := NewDockerClient(endpoint); err == nil {
			t.Errorf("unsafe Docker endpoint accepted: %s", endpoint)
		}
	}
}

type fakeSystemd struct {
	queried   []string
	unit      dbus.UnitStatus
	operation string
}

func (f *fakeSystemd) ListUnitsByNamesContext(_ context.Context, names []string) ([]dbus.UnitStatus, error) {
	f.queried = append([]string(nil), names...)
	return []dbus.UnitStatus{f.unit}, nil
}
func (f *fakeSystemd) StartUnitContext(_ context.Context, name, mode string, result chan<- string) (int, error) {
	f.operation = "start:" + name + ":" + mode
	result <- "done"
	return 1, nil
}
func (f *fakeSystemd) StopUnitContext(_ context.Context, name, mode string, result chan<- string) (int, error) {
	f.operation = "stop:" + name + ":" + mode
	result <- "done"
	return 1, nil
}
func (f *fakeSystemd) RestartUnitContext(_ context.Context, name, mode string, result chan<- string) (int, error) {
	f.operation = "restart:" + name + ":" + mode
	result <- "done"
	return 1, nil
}

func TestSystemdBackendQueriesAndControlsOneExactAllowlistedUnit(t *testing.T) {
	unitName := "demo-worker.service"
	fake := &fakeSystemd{unit: dbus.UnitStatus{Name: unitName, ActiveState: "active"}}
	backend := &SystemdBackend{conn: fake}
	target := Target{Kind: KindSystemd, Identity: unitName}
	state, err := backend.Status(context.Background(), target)
	if err != nil || state != StateRunning || len(fake.queried) != 1 || fake.queried[0] != unitName {
		t.Fatalf("status=%s query=%v err=%v", state, fake.queried, err)
	}
	if err := backend.Transition(context.Background(), target, ActionRestart); err != nil {
		t.Fatal(err)
	}
	if fake.operation != "restart:"+unitName+":fail" {
		t.Fatalf("systemd operation=%q", fake.operation)
	}
	target.Identity = "projecthub-demo;rm.service"
	if _, err := backend.Status(context.Background(), target); !errors.Is(err, ErrIdentityMismatch) {
		t.Fatalf("invalid unit error=%v", err)
	}
}
