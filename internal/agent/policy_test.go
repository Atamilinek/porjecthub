package agent

import (
	"strings"
	"testing"
)

func TestParsePolicyRejectsUnknownOrDangerousTargets(t *testing.T) {
	validID := strings.Repeat("a", 64)
	valid := `{"agent_id":"agent-west","targets":[{"id":"catalog-api","kind":"docker_container","identity":"` + validID + `","actions":["status","restart"],"protected":false}]}`
	policy, err := ParsePolicy(strings.NewReader(valid))
	if err != nil || policy.AgentID() != "agent-west" {
		t.Fatalf("valid policy=%v err=%v", policy, err)
	}
	for _, invalid := range []string{
		`{"agent_id":"a","targets":[{"id":"x","kind":"docker_container","identity":"short","actions":["status","stop"],"protected":false}]}`,
		`{"agent_id":"a","targets":[{"id":"x","kind":"docker_container","identity":"` + validID + `","actions":["status","shell"],"protected":false}]}`,
		`{"agent_id":"a","targets":[{"id":"x","kind":"systemd_unit","identity":"projecthub-api.service","actions":["status","restart"],"protected":true}]}`,
		`{"agent_id":"a","targets":[{"id":"x","kind":"systemd_unit","identity":"projecthub-api.service","actions":["status","restart"],"protected":false}]}`,
		`{"agent_id":"a","targets":[{"id":"x","kind":"docker_container","identity":"` + validID + `","actions":["status"],"protected":false,"arbitrary_command":"id"}]}`,
		`{"agent_id":"a","targets":[{"id":"x","kind":"shell","identity":"id","actions":["status"],"protected":false}]}`,
	} {
		if _, err := ParsePolicy(strings.NewReader(invalid)); err == nil {
			t.Errorf("accepted unsafe policy: %s", invalid)
		}
	}
}
