package monitor

import "testing"

func TestFailureAndRecoveryThresholdStateMachine(t *testing.T) {
	cases := []struct {
		name                                string
		state                               State
		success                             bool
		failures, recoveries                int
		failureThreshold, recoveryThreshold int
		want                                State
		wantFailures, wantRecoveries        int
	}{
		{"first failure degrades", StateUnknown, false, 0, 0, 3, 2, StateDegraded, 1, 0},
		{"healthy failure degrades", StateHealthy, false, 0, 0, 3, 2, StateDegraded, 1, 0},
		{"failure threshold goes offline", StateDegraded, false, 2, 0, 3, 2, StateOffline, 3, 0},
		{"first recovery remains degraded", StateOffline, true, 3, 0, 3, 2, StateDegraded, 0, 1},
		{"recovery threshold heals", StateDegraded, true, 0, 1, 3, 2, StateHealthy, 0, 0},
		{"healthy success resets counters", StateHealthy, true, 2, 1, 3, 2, StateHealthy, 0, 0},
		{"agent loss is explicit", StateHealthy, false, 0, 0, 3, 2, StateAgentUnreachable, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Transition(tc.state, tc.success, tc.failures, tc.recoveries, tc.failureThreshold, tc.recoveryThreshold, tc.name == "agent loss is explicit")
			if got.State != tc.want || got.Failures != tc.wantFailures || got.Recoveries != tc.wantRecoveries {
				t.Fatalf("Transition()=%+v, want state=%s failures=%d recoveries=%d", got, tc.want, tc.wantFailures, tc.wantRecoveries)
			}
		})
	}
}
