package permission

import (
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
)

func TestLoadEmptyPresetReturnsDefault(t *testing.T) {
	pf, err := LoadPreset(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatal(err)
	}
	if pf.SchemaVersion != 1 {
		t.Errorf("schema_version = %d, want 1", pf.SchemaVersion)
	}
	if len(pf.Rules) != 0 {
		t.Errorf("rules = %v", pf.Rules)
	}
}

func TestPromoteSessionRuleToPreset(t *testing.T) {
	dir := t.TempDir()
	presetPath := filepath.Join(dir, "preset.json")

	e := New(nil)
	sess := ids.NewSessionID()
	e.AddSessionRule(sess, Rule{Tool: "Bash", ArgvGlob: "git push *", Action: DecisionAllow})

	promoted, err := e.Promote(presetPath, sess, 0)
	if err != nil {
		t.Fatal(err)
	}
	if promoted.Tool != "Bash" || promoted.ArgvGlob != "git push *" {
		t.Errorf("promoted = %+v", promoted)
	}

	// File contains the rule.
	pf, err := LoadPreset(presetPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(pf.Rules) != 1 || pf.Rules[0].Tool != "Bash" {
		t.Errorf("preset = %+v", pf)
	}

	// Engine's session rules no longer include it; profile rules do.
	if got := e.ListSessionRules(sess); len(got) != 0 {
		t.Errorf("session rules still contain promoted rule: %v", got)
	}
	if got := e.Evaluate(sess, "Bash", "git push origin main", true); got != DecisionAllow {
		t.Errorf("profile rule not applied: got %s", got)
	}
}

func TestPromoteByDescription(t *testing.T) {
	dir := t.TempDir()
	presetPath := filepath.Join(dir, "preset.json")
	e := New(nil)
	sess := ids.NewSessionID()
	e.AddSessionRule(sess, Rule{Tool: "Write", Action: DecisionAllow})
	if _, err := e.PromoteByDescription(presetPath, sess, "Write"); err != nil {
		t.Fatal(err)
	}
	pf, _ := LoadPreset(presetPath)
	if len(pf.Rules) != 1 {
		t.Errorf("preset rules len = %d", len(pf.Rules))
	}
}

func TestPromoteDeduplicates(t *testing.T) {
	dir := t.TempDir()
	presetPath := filepath.Join(dir, "preset.json")
	_ = SavePreset(presetPath, &PresetFile{
		SchemaVersion: 1,
		Rules:         []Rule{{Tool: "Bash", ArgvGlob: "git status*"}},
	})

	e := New(nil)
	sess := ids.NewSessionID()
	e.AddSessionRule(sess, Rule{Tool: "Bash", ArgvGlob: "git status*", Action: DecisionAllow})
	if _, err := e.Promote(presetPath, sess, 0); err != nil {
		t.Fatal(err)
	}
	pf, _ := LoadPreset(presetPath)
	if len(pf.Rules) != 1 {
		t.Errorf("dedup failed: %+v", pf)
	}
}
