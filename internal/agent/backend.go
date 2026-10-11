package agent

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"

	"github.com/coreos/go-systemd/v22/dbus"
	containertypes "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

type DockerEngine interface {
	ContainerInspect(context.Context, string, client.ContainerInspectOptions) (client.ContainerInspectResult, error)
	ContainerStart(context.Context, string, client.ContainerStartOptions) (client.ContainerStartResult, error)
	ContainerStop(context.Context, string, client.ContainerStopOptions) (client.ContainerStopResult, error)
	ContainerRestart(context.Context, string, client.ContainerRestartOptions) (client.ContainerRestartResult, error)
}

type DockerBackend struct{ engine DockerEngine }

func NewDockerBackend(engine DockerEngine) *DockerBackend { return &DockerBackend{engine: engine} }
func NewDockerClient(host string) (*client.Client, error) {
	if host == "" {
		return nil, errors.New("Docker endpoint must be explicitly configured")
	}
	endpoint, err := url.Parse(host)
	if err != nil || endpoint.Scheme != "unix" || endpoint.Host != "" || !path.IsAbs(endpoint.Path) || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return nil, errors.New("Docker endpoint must be an explicit local unix socket")
	}
	return client.NewClientWithOpts(client.WithHost(host), client.WithAPIVersionNegotiation())
}
func (b *DockerBackend) Status(ctx context.Context, target Target) (State, error) {
	response, err := b.inspect(ctx, target)
	if err != nil {
		return StateUnknown, err
	}
	if response.Container.State.Running {
		return StateRunning, nil
	}
	return StateStopped, nil
}
func (b *DockerBackend) Transition(ctx context.Context, target Target, action Action) error {
	if target.Kind != KindDocker || !validDockerID(target.Identity) {
		return ErrIdentityMismatch
	}
	if target.Protected {
		return ErrProtectedTarget
	}
	response, err := b.inspect(ctx, target)
	if err != nil {
		return err
	}
	if protectedContainer(response.Container) {
		return ErrProtectedTarget
	}
	state := StateStopped
	if response.Container.State.Running {
		state = StateRunning
	}
	if action == ActionStart && state == StateRunning || action == ActionStop && state == StateStopped {
		return nil
	}
	switch action {
	case ActionStart:
		_, err = b.engine.ContainerStart(ctx, target.Identity, client.ContainerStartOptions{})
	case ActionStop:
		seconds := 5
		_, err = b.engine.ContainerStop(ctx, target.Identity, client.ContainerStopOptions{Timeout: &seconds})
	case ActionRestart:
		seconds := 5
		_, err = b.engine.ContainerRestart(ctx, target.Identity, client.ContainerRestartOptions{Timeout: &seconds})
	default:
		return ErrUnsupportedAction
	}
	return err
}
func (b *DockerBackend) inspect(ctx context.Context, target Target) (client.ContainerInspectResult, error) {
	if target.Kind != KindDocker || !validDockerID(target.Identity) {
		return client.ContainerInspectResult{}, ErrIdentityMismatch
	}
	if b == nil || b.engine == nil {
		return client.ContainerInspectResult{}, errors.New("Docker adapter unavailable")
	}
	response, err := b.engine.ContainerInspect(ctx, target.Identity, client.ContainerInspectOptions{})
	if err != nil {
		return client.ContainerInspectResult{}, err
	}
	if response.Container.ID != target.Identity || response.Container.State == nil {
		return client.ContainerInspectResult{}, ErrIdentityMismatch
	}
	return response, nil
}
func protectedContainer(container containertypes.InspectResponse) bool {
	name := strings.TrimPrefix(strings.ToLower(container.Name), "/")
	if strings.HasPrefix(name, "projecthub-") {
		return true
	}
	if container.Config != nil && (container.Config.Labels["com.projecthub.protected"] == "true" || container.Config.Labels["projecthub.protected"] == "true") {
		return true
	}
	return false
}

type systemdController interface {
	ListUnitsByNamesContext(context.Context, []string) ([]dbus.UnitStatus, error)
	StartUnitContext(context.Context, string, string, chan<- string) (int, error)
	StopUnitContext(context.Context, string, string, chan<- string) (int, error)
	RestartUnitContext(context.Context, string, string, chan<- string) (int, error)
}
type SystemdBackend struct{ conn systemdController }

func NewSystemdBackend(ctx context.Context) (*SystemdBackend, error) {
	conn, err := dbus.NewSystemConnectionContext(ctx)
	if err != nil {
		return nil, err
	}
	return &SystemdBackend{conn: conn}, nil
}
func (b *SystemdBackend) Close() {
	if conn, ok := b.conn.(*dbus.Conn); ok {
		conn.Close()
	}
}
func (b *SystemdBackend) Status(ctx context.Context, target Target) (State, error) {
	if target.Kind != KindSystemd || !validUnit(target.Identity) {
		return StateUnknown, ErrIdentityMismatch
	}
	if b == nil || b.conn == nil {
		return StateUnknown, errors.New("systemd adapter unavailable")
	}
	units, err := b.conn.ListUnitsByNamesContext(ctx, []string{target.Identity})
	if err != nil {
		return StateUnknown, err
	}
	for _, unit := range units {
		if unit.Name != target.Identity {
			continue
		}
		switch unit.ActiveState {
		case "active", "activating", "reloading":
			return StateRunning, nil
		case "inactive", "failed", "deactivating":
			return StateStopped, nil
		default:
			return StateUnknown, nil
		}
	}
	return StateUnknown, ErrTargetNotFound
}
func (b *SystemdBackend) Transition(ctx context.Context, target Target, action Action) error {
	if target.Kind != KindSystemd || !validUnit(target.Identity) {
		return ErrIdentityMismatch
	}
	if target.Protected || hardProtectedSystemd(target) {
		return ErrProtectedTarget
	}
	if _, err := b.Status(ctx, target); err != nil {
		return err
	}
	job := make(chan string, 1)
	var err error
	switch action {
	case ActionStart:
		_, err = b.conn.StartUnitContext(ctx, target.Identity, "fail", job)
	case ActionStop:
		_, err = b.conn.StopUnitContext(ctx, target.Identity, "fail", job)
	case ActionRestart:
		_, err = b.conn.RestartUnitContext(ctx, target.Identity, "fail", job)
	default:
		return ErrUnsupportedAction
	}
	if err != nil {
		return err
	}
	select {
	case result := <-job:
		if result != "done" && result != "skipped" {
			return fmt.Errorf("systemd job %s: %s", target.Identity, result)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type BackendMux struct {
	Docker  *DockerBackend
	Systemd *SystemdBackend
}

func (m BackendMux) backend(kind Kind) Backend {
	switch kind {
	case KindDocker:
		return m.Docker
	case KindSystemd:
		return m.Systemd
	default:
		return nil
	}
}
func (m BackendMux) Status(ctx context.Context, target Target) (State, error) {
	b := m.backend(target.Kind)
	if b == nil {
		return StateUnknown, errors.New("target backend is not configured")
	}
	return b.Status(ctx, target)
}
func (m BackendMux) Transition(ctx context.Context, target Target, action Action) error {
	b := m.backend(target.Kind)
	if b == nil {
		return errors.New("target backend is not configured")
	}
	return b.Transition(ctx, target, action)
}
