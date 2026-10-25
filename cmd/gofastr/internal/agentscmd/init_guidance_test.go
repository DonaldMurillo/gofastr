package agentscmd

import (
	"testing"
)

func TestBuildAgentsMDTeachesPublicEscapeHatch(t *testing.T) {
	// Coding agents read AGENTS.md first; without the one-liner they
	// dead-end on the same 401 a human newcomer does.
	for _, want := range []string{"Public: true", "battery/auth", "gofastr docs auth"} {
		mustContain(t, string(BuildAgentsMD()), want)
	}
}
