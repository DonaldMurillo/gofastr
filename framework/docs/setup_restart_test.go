package docs

import (
	"os"
	"strings"
	"testing"
)

//gofastr:test-reads battery/setup/agents.md framework/docs/content/first-run.md
func TestSetupRestartDocsStateBoundary(t *testing.T) {
	guide, err := Get("first-run")
	if err != nil {
		t.Fatal(err)
	}
	agent, err := os.ReadFile("../../battery/setup/agents.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, doc := range []string{string(guide), string(agent)} {
		for _, claim := range []string{"a crash mid-setup\nsimply re-enters setup", "A crash mid-setup\n  re-enters setup"} {
			if strings.Contains(doc, claim) {
				t.Error("unconditional crash-resume claim remains")
			}
		}
		if !strings.Contains(doc, "remaining steps") || !strings.Contains(doc, "true") {
			t.Error("restart guidance omits the Complete boundary and remaining steps")
		}
	}
}
