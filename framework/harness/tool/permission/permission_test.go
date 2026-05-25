package permission

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
)

func TestQuietModeAllowsReadOnlyTools(t *testing.T) {
	e := New(nil)
	for _, name := range []string{"Read", "Glob", "Ls", "Grep"} {
		if got := e.Evaluate(ids.NewSessionID(), name, "", false); got != DecisionAllow {
			t.Errorf("%s mutating=false → %s, want allow", name, got)
		}
	}
}

func TestQuietModeAllowsSafeBash(t *testing.T) {
	e := New(nil)
	cases := []string{
		"git status",
		"git log -n 5",
		"git diff HEAD",
		"ls -la",
		"pwd",
		"cat /etc/hosts",
		"grep -R foo .",
		"find . -name *.go",
	}
	for _, cmd := range cases {
		if got := e.Evaluate(ids.NewSessionID(), "Bash", cmd, true); got != DecisionAllow {
			t.Errorf("quiet-mode should allow %q, got %s", cmd, got)
		}
	}
}

func TestUnsafeBashAsks(t *testing.T) {
	e := New(nil)
	if got := e.Evaluate(ids.NewSessionID(), "Bash", "rm -rf /tmp/x", true); got != DecisionAsk {
		t.Errorf("got %s, want ask", got)
	}
}

func TestStrictPermissionsAsksReadOnly(t *testing.T) {
	e := New(nil)
	e.StrictPermissions = true
	if got := e.Evaluate(ids.NewSessionID(), "Read", "Read:/etc/hosts", false); got != DecisionAsk {
		t.Errorf("strict mode allowed Read: got %s", got)
	}
}

func TestSessionRuleApplies(t *testing.T) {
	e := New(nil)
	sess := ids.NewSessionID()
	e.AddSessionRule(sess, Rule{Tool: "Bash", ArgvGlob: "git push *", Action: DecisionAllow})
	if got := e.Evaluate(sess, "Bash", "git push origin main", true); got != DecisionAllow {
		t.Errorf("session rule did not apply: got %s", got)
	}
	// Different session: no allow.
	if got := e.Evaluate(ids.NewSessionID(), "Bash", "git push origin main", true); got == DecisionAllow {
		t.Errorf("session rule leaked across sessions")
	}
}

func TestSessionRuleSurvivesRevoke(t *testing.T) {
	e := New(nil)
	sess := ids.NewSessionID()
	e.AddSessionRule(sess, Rule{Tool: "Bash", Action: DecisionAllow})
	if len(e.ListSessionRules(sess)) != 1 {
		t.Fatal("rule not added")
	}
	e.RevokeSessionRule(sess, 0)
	if len(e.ListSessionRules(sess)) != 0 {
		t.Fatal("rule not revoked")
	}
}

func TestAnswerToRuleScopes(t *testing.T) {
	cases := []struct {
		ans  control.AnswerPermission
		want bool
	}{
		{control.AnswerPermission{Scope: control.ScopeOnce}, false},
		{control.AnswerPermission{Scope: control.ScopeArgvGlob, Decision: control.DecisionAllow}, true},
		{control.AnswerPermission{Scope: control.ScopeTool, Decision: control.DecisionAllow}, true},
		{control.AnswerPermission{Scope: control.ScopeSessionWide, Decision: control.DecisionAllow}, true},
	}
	for _, c := range cases {
		_, ok := AnswerToRule("Bash", "git push *", c.ans)
		if ok != c.want {
			t.Errorf("AnswerToRule scope=%q produced rule=%v, want %v", c.ans.Scope, ok, c.want)
		}
	}
}

func TestProfileRuleFallback(t *testing.T) {
	e := New([]Rule{{Tool: "Write", Action: DecisionDeny}})
	if got := e.Evaluate(ids.NewSessionID(), "Write", "Write:/etc/passwd", true); got != DecisionDeny {
		t.Errorf("profile rule didn't apply: got %s", got)
	}
}
