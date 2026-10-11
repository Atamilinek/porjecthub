package monitor

type State string

const (
	StateChecking         State = "checking"
	StateHealthy          State = "healthy"
	StateDegraded         State = "degraded"
	StateOffline          State = "offline"
	StateUnknown          State = "unknown"
	StateAgentUnreachable State = "agent_unreachable"
)

type TransitionResult struct {
	State      State
	Failures   int
	Recoveries int
}

func Transition(current State, success bool, failures, recoveries, failureThreshold, recoveryThreshold int, agentUnreachable bool) TransitionResult {
	if agentUnreachable {
		return TransitionResult{State: StateAgentUnreachable}
	}
	if failureThreshold < 1 {
		failureThreshold = 1
	}
	if recoveryThreshold < 1 {
		recoveryThreshold = 1
	}
	if success {
		if current == StateUnknown || current == StateChecking {
			return TransitionResult{State: StateHealthy}
		}
		if current == StateHealthy {
			return TransitionResult{State: StateHealthy}
		}
		recoveries++
		if recoveries >= recoveryThreshold {
			return TransitionResult{State: StateHealthy}
		}
		return TransitionResult{State: StateDegraded, Recoveries: recoveries}
	}
	failures++
	if failures >= failureThreshold {
		return TransitionResult{State: StateOffline, Failures: failures}
	}
	return TransitionResult{State: StateDegraded, Failures: failures}
}
