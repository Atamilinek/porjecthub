package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
)

type Action string

const (
	ActionStatus  Action = "status"
	ActionStart   Action = "start"
	ActionStop    Action = "stop"
	ActionRestart Action = "restart"
)

type Kind string

const (
	KindDocker  Kind = "docker_container"
	KindSystemd Kind = "systemd_unit"
)

type State string

const (
	StateRunning State = "running"
	StateStopped State = "stopped"
	StateUnknown State = "unknown"
)

type Target struct {
	ID        string   `json:"id"`
	Kind      Kind     `json:"kind"`
	Identity  string   `json:"identity"`
	Actions   []Action `json:"actions"`
	Protected bool     `json:"protected"`
}
type PolicyFile struct {
	AgentID string   `json:"agent_id"`
	Targets []Target `json:"targets"`
}
type Policy struct {
	agentID string
	targets map[string]Target
}

var safeID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:-]{0,127}$`)

var (
	ErrInvalidPolicy      = errors.New("invalid agent policy")
	ErrUnsupportedAction  = errors.New("unsupported action")
	ErrProtectedTarget    = errors.New("protected target")
	ErrTargetNotFound     = errors.New("target not found")
	ErrRequestConflict    = errors.New("request ID reused with different action")
	ErrAgentIdentity      = errors.New("agent identity mismatch")
	ErrIdentityMismatch   = errors.New("target identity mismatch")
	ErrStateMismatch      = errors.New("target state does not match requested action")
	ErrTargetKindMismatch = errors.New("target kind mismatch")
)

func NewPolicy(agentID string, targets []Target) *Policy {
	return &Policy{agentID: agentID, targets: func() map[string]Target {
		result := make(map[string]Target, len(targets))
		for _, target := range targets {
			result[target.ID] = cloneTarget(target)
		}
		return result
	}()}
}

func ParsePolicy(reader io.Reader) (*Policy, error) {
	decoder := json.NewDecoder(io.LimitReader(reader, 1<<20))
	decoder.DisallowUnknownFields()
	var file PolicyFile
	if err := decoder.Decode(&file); err != nil {
		return nil, fmt.Errorf("decode agent policy: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("decode agent policy trailing data: %w", ErrInvalidPolicy)
	}
	if !safeID.MatchString(file.AgentID) || len(file.Targets) > 10000 {
		return nil, ErrInvalidPolicy
	}
	targets := make([]Target, 0, len(file.Targets))
	seen := make(map[string]bool, len(file.Targets))
	for _, target := range file.Targets {
		if !safeID.MatchString(target.ID) || target.ID == file.AgentID || seen[target.ID] || target.Identity == "" || len(target.Identity) > 512 {
			return nil, ErrInvalidPolicy
		}
		if target.Kind != KindDocker && target.Kind != KindSystemd {
			return nil, ErrInvalidPolicy
		}
		seen[target.ID] = true
		actions := make(map[Action]bool, len(target.Actions))
		for _, action := range target.Actions {
			if action != ActionStatus && action != ActionStart && action != ActionStop && action != ActionRestart {
				return nil, ErrUnsupportedAction
			}
			if actions[action] {
				return nil, ErrInvalidPolicy
			}
			actions[action] = true
		}
		if len(actions) == 0 || !actions[ActionStatus] || (target.Protected || hardProtectedSystemd(target)) && (actions[ActionStart] || actions[ActionStop] || actions[ActionRestart]) {
			return nil, ErrInvalidPolicy
		}
		if target.Kind == KindDocker && !validDockerID(target.Identity) {
			return nil, ErrInvalidPolicy
		}
		if target.Kind == KindSystemd && !validUnit(target.Identity) {
			return nil, ErrInvalidPolicy
		}
		targets = append(targets, target)
	}
	return NewPolicy(file.AgentID, targets), nil
}

func (p *Policy) AgentID() string {
	if p == nil {
		return ""
	}
	return p.agentID
}
func (p *Policy) TargetIDs() []string {
	if p == nil {
		return nil
	}
	ids := make([]string, 0, len(p.targets))
	for id := range p.targets {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
func (p *Policy) Target(id string) (Target, bool) {
	if p == nil {
		return Target{}, false
	}
	target, ok := p.targets[id]
	return cloneTarget(target), ok
}
func cloneTarget(target Target) Target {
	target.Actions = append([]Action(nil), target.Actions...)
	return target
}
func validDockerID(value string) bool {
	return len(value) == 64 && regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(strings.TrimSpace(value))
}
func validUnit(value string) bool {
	return len(value) > 8 && len(value) <= 255 && regexp.MustCompile(`^[a-zA-Z0-9_@.:-]+\.service$`).MatchString(value)
}

func hardProtectedSystemd(target Target) bool {
	if target.Kind != KindSystemd {
		return false
	}
	name := strings.ToLower(target.Identity)
	if strings.HasPrefix(name, "projecthub-") {
		return true
	}
	switch name {
	case "projecthub-api.service", "projecthub-agent.service", "docker.service", "containerd.service", "sshd.service", "ssh.service", "systemd-logind.service", "systemd-networkd.service", "dbus.service", "polkit.service", "networkmanager.service", "firewalld.service", "ufw.service":
		return true
	}
	return false
}
