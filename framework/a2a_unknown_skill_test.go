package framework

import (
	"net/http"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/a2a"
)

// A message naming an unknown skill was rejected with "available:" and
// every registered skill id, the entity skills the MCP gate withholds
// from the caller (and from the public card) included (2026-10-27 bug
// bash P24). The refusal names only the skill the caller asked for.
func TestA2AUnknownSkillListsNothing(t *testing.T) {
	env := newA2ATestEnv(t, A2AConfig{Skills: []a2a.Skill{a2aEchoSkill()}}, WithMCPGate(a2aAdminOnlyGate))
	msg := strings.Replace(a2aSendMessage("list", map[string]any{}), `"entity.notes"`, `"zzz"`, 1)
	code, body := env.a2aRequest(t, "tok-alice", msg)
	if code != http.StatusOK {
		t.Fatalf("SendMessage: got %d body %s", code, body)
	}
	if got := taskState(a2aTask(t, body)); got != "TASK_STATE_REJECTED" {
		t.Fatalf("state = %s, want REJECTED: %s", got, body)
	}
	if strings.Contains(string(body), "entity.notes") {
		t.Errorf("SECURITY: [disclosure] an unknown-skill refusal named the gated entity skill: %s", body)
	}
	if !strings.Contains(string(body), `no skill named \"zzz\"`) {
		t.Errorf("the refusal must name the skill asked for: %s", body)
	}
}
