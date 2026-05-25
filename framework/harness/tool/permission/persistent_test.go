package permission

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
)

// TestAnswerToRule_ScopeAlways covers the wire-format mapping:
// AnswerPermission{Scope: "always"} → a Rule + the "persist to disk"
// signal so callers know to save it.
func TestAnswerToRule_ScopeAlways(t *testing.T) {
	ans := control.AnswerPermission{
		Decision: control.DecisionAllow,
		Scope:    control.ScopeAlways,
	}
	rule, persist, ok := AnswerToRuleWithPersist("Bash", "git status", ans)
	if !ok {
		t.Fatal("ScopeAlways should produce a Rule")
	}
	if !persist {
		t.Error("ScopeAlways should be flagged for persistence")
	}
	if rule.Action != DecisionAllow {
		t.Errorf("rule action = %v, want Allow", rule.Action)
	}
	if rule.Tool != "Bash" {
		t.Errorf("rule tool = %q, want Bash", rule.Tool)
	}
}

// TestAnswerToRule_ScopeOnce_NoPersist: scopes other than Always
// must not be flagged for persistence.
func TestAnswerToRule_ScopeOnce_NoPersist(t *testing.T) {
	for _, sc := range []control.PermitScope{
		control.ScopeOnce, control.ScopeTool, control.ScopeSessionWide,
	} {
		_, persist, _ := AnswerToRuleWithPersist("Bash", "",
			control.AnswerPermission{Decision: control.DecisionAllow, Scope: sc})
		if persist {
			t.Errorf("scope %q was flagged for persistence — only ScopeAlways should be", sc)
		}
	}
}

// TestPersistentRulesSurviveEngineRestart: save a persistent rule via
// one Engine instance, instantiate a NEW Engine pointed at the same
// file, verify Evaluate honors the loaded rule without prompting.
// This is the core "Allow always" contract.
func TestPersistentRulesSurviveEngineRestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "permissions.json")

	e1 := New(nil)
	e1.PersistencePath = path
	if err := e1.AddPersistentRule(Rule{
		Tool: "Bash", ArgvGlob: "git status", Action: DecisionAllow,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("persistence file not written: %v", err)
	}

	e2 := New(nil)
	e2.PersistencePath = path
	if err := e2.LoadPersistentRules(); err != nil {
		t.Fatal(err)
	}
	e2.StrictPermissions = true // would normally Ask for Bash
	got := e2.Evaluate(ids.NewSessionID(), "Bash", "git status", true)
	if got != DecisionAllow {
		t.Errorf("persistent rule not honored: got %v, want Allow", got)
	}
}

// TestPersistentRulesScopedByTool: a Bash persistent rule must NOT
// leak into Write decisions.
func TestPersistentRulesScopedByTool(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "permissions.json")
	e := New(nil)
	e.PersistencePath = path
	if err := e.AddPersistentRule(Rule{
		Tool: "Bash", ArgvGlob: "*", Action: DecisionAllow,
	}); err != nil {
		t.Fatal(err)
	}
	e.StrictPermissions = true
	got := e.Evaluate(ids.NewSessionID(), "Write", "/tmp/x", true)
	if got == DecisionAllow {
		t.Errorf("Bash persistent rule leaked into Write: got %v", got)
	}
}

// TestLoadPersistentRulesOnMissingFileIsNoError: first-time boot.
func TestLoadPersistentRulesOnMissingFileIsNoError(t *testing.T) {
	e := New(nil)
	e.PersistencePath = filepath.Join(t.TempDir(), "nonexistent.json")
	if err := e.LoadPersistentRules(); err != nil {
		t.Errorf("missing file should NOT be an error on load: %v", err)
	}
}

// TestAddPersistentRuleAtomicWrite: the file write must be atomic
// (no torn JSON on crash). We just verify the format parses round-trip
// after multiple adds.
func TestAddPersistentRule_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "permissions.json")
	e1 := New(nil)
	e1.PersistencePath = path
	for _, r := range []Rule{
		{Tool: "Bash", ArgvGlob: "git status", Action: DecisionAllow},
		{Tool: "Read", ArgvGlob: "", Action: DecisionAllow},
		{Tool: "Write", ArgvGlob: "/tmp/*", Action: DecisionDeny},
	} {
		if err := e1.AddPersistentRule(r); err != nil {
			t.Fatal(err)
		}
	}
	e2 := New(nil)
	e2.PersistencePath = path
	if err := e2.LoadPersistentRules(); err != nil {
		t.Fatal(err)
	}
	rules := e2.ListPersistentRules()
	if len(rules) != 3 {
		t.Errorf("round-trip lost rules: got %d, want 3: %+v", len(rules), rules)
	}
}
