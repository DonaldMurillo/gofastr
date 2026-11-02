package permission

import (
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
)

// TestAllowAlwaysStaysInItsWorkingDirectory pins that an "Allow always"
// rule covers the working directory it was granted in, not every repo
// the harness later runs in. The rule file is one per user
// ($XDG_CONFIG_HOME/gofastr/harness/permissions.json) and a summary
// leaves the working directory implicit: approving `go test ./...`
// forever in your own repo also ran `go test ./...` with no prompt in
// a cloned one, which executes that repo's test code, and Grep:TODO
// approved over one checkout covered every other (round 2 scoped only
// an explicit --path).
func TestAllowAlwaysStaysInItsWorkingDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "permissions.json")
	own, cloned := t.TempDir(), t.TempDir()
	sess := ids.NewSessionID()

	granting := New(nil)
	granting.PersistencePath = path
	granting.WorkingDir = own
	if err := granting.AddPersistentRule(Rule{Tool: "Bash", ArgvGlob: "go test ./...", Action: DecisionAllow}); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		dir  string
		want Decision
	}{
		{own, DecisionAllow},
		{cloned, DecisionAsk},
	} {
		e := New(nil)
		e.PersistencePath = path
		e.WorkingDir = tc.dir
		if err := e.LoadPersistentRules(); err != nil {
			t.Fatal(err)
		}
		if got := e.Evaluate(sess, "Bash", "go test ./...", true); got != tc.want {
			t.Errorf("working dir %s: Evaluate = %v, want %v", tc.dir, got, tc.want)
		}
	}

	// A rule saved before rules carried a directory names none: it is
	// not honoured in a working directory, so it prompts once more.
	legacy := New(nil)
	legacy.WorkingDir = own
	legacy.persistentRules = []Rule{{Tool: "Bash", ArgvGlob: "go test ./...", Action: DecisionAllow}}
	if got := legacy.Evaluate(sess, "Bash", "go test ./...", true); got != DecisionAsk {
		t.Errorf("legacy directory-less rule: Evaluate = %v, want ask", got)
	}
}
