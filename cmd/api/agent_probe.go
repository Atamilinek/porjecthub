package main

import (
	"context"
	"errors"
	"time"

	"github.com/projecthub/projecthub/internal/agent"
	"github.com/projecthub/projecthub/internal/jobs"
	"github.com/projecthub/projecthub/internal/monitor"
)

type monitorAgentProbe struct{ dispatcher *jobs.Dispatcher }

func (p monitorAgentProbe) CheckAgent(ctx context.Context, targetID string) (monitor.ProbeResult, error) {
	started := time.Now()
	result, err := p.dispatcher.Status(ctx, targetID)
	latency := time.Since(started)
	if err != nil {
		if errors.Is(err, agent.ErrAgentUnavailable) {
			return monitor.ProbeResult{Latency: latency, Detail: "agent unreachable"}, monitor.ErrAgentUnavailable
		}
		return monitor.ProbeResult{Latency: latency, Detail: "agent status failed"}, err
	}
	if agent.State(result.State) != agent.StateRunning {
		return monitor.ProbeResult{Latency: latency, Detail: "target is not running"}, errors.New("agent target is not running")
	}
	return monitor.ProbeResult{OK: true, Latency: latency}, nil
}
