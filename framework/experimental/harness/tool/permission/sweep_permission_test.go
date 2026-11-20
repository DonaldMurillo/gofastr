package permission

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
)

func TestPromoteDenyOverridesPresetAllow(t *testing.T) {
	presetPath := filepath.Join(t.TempDir(), "preset.json")
	if err := SavePreset(presetPath, &PresetFile{Rules: []Rule{{Tool: "Bash", ArgvGlob: "make deploy", Action: DecisionAllow}}}); err != nil {
		t.Fatal(err)
	}
	pf, _ := LoadPreset(presetPath)
	e := New(pf.Rules)
	sess := ids.NewSessionID()
	e.AddSessionRule(sess, Rule{Tool: "Bash", ArgvGlob: "make deploy", Action: DecisionDeny})
	got, err := e.Promote(presetPath, sess, 0)
	if err != nil || got.Action != DecisionDeny {
		t.Fatalf("Promote = %+v, %v", got, err)
	}
	pf, _ = LoadPreset(presetPath)
	if len(pf.Rules) != 1 || pf.Rules[0].Action != DecisionDeny {
		t.Fatalf("preset after promoting a deny = %+v, want the one rule to deny", pf.Rules)
	}
	if d := e.Evaluate(ids.NewSessionID(), "Bash", "make deploy", true); d != DecisionDeny {
		t.Fatalf("Evaluate after promoting a deny = %v, want deny", d)
	}
	if n := len(e.ListSessionRules(sess)); n != 0 {
		t.Fatalf("promoted rule still in session scope (%d rules)", n)
	}
}

func TestRuleFilesAreOwnerOnly(t *testing.T) {
	dir := t.TempDir()
	presetPath := filepath.Join(dir, "p", "preset.json")
	if err := SavePreset(presetPath, &PresetFile{Rules: []Rule{{Tool: "Read", Action: DecisionAllow}}}); err != nil {
		t.Fatal(err)
	}
	e := New(nil)
	e.PersistencePath = filepath.Join(dir, "q", "permissions.json")
	if err := e.AddPersistentRule(Rule{Tool: "Read", Action: DecisionAllow}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{presetPath, e.PersistencePath} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o077 != 0 {
			t.Errorf("%s mode %v, want owner-only", p, info.Mode().Perm())
		}
		if dinfo, _ := os.Stat(filepath.Dir(p)); dinfo.Mode().Perm()&0o077 != 0 {
			t.Errorf("%s dir mode %v, want owner-only", p, dinfo.Mode().Perm())
		}
	}
}

func TestAllowAlwaysAnswerMintsLiteral(t *testing.T) {
	rule, persist, ok := AnswerToRuleWithPersist("Bash", "git diff *", control.AnswerPermission{Scope: control.ScopeAlways, Decision: control.DecisionAllow})
	if !ok || !persist {
		t.Fatalf("ScopeAlways gave ok=%v persist=%v", ok, persist)
	}
	if rule.Glob || rule.Match("Bash", "git diff ; nc attacker 9") {
		t.Fatal("an Allow always answer minted a pattern rule that matches a chained command")
	}
}

func TestQuietModeAsksForDotDotThroughASymlink(t *testing.T) {
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(outside, "inner"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	if err := os.Symlink(filepath.Join(outside, "inner"), filepath.Join(base, "link")); err != nil {
		t.Skip(err)
	}
	e := New(nil)
	e.WorkingDir = base
	for _, c := range []struct{ tool, argv string }{{"Bash", "cat link/../secret"}, {"Read", "Read:link/../secret"}} {
		if got := e.Evaluate(ids.NewSessionID(), c.tool, c.argv, false); got != DecisionAsk {
			t.Errorf("%s %q = %v, want ask: the kernel resolves link/.. outside the working dir", c.tool, c.argv, got)
		}
	}
}

func TestPromoteRefusesShadowedRule(t *testing.T) {
	deny := Rule{Tool: "Bash", ArgvGlob: "make deploy", Action: DecisionDeny}
	cases := map[string]func(e *Engine, presetPath string){
		"persistent allow in working dir": func(e *Engine, _ string) {
			e.WorkingDir = "/repo"
			if err := e.AddPersistentRule(Rule{Tool: "Bash", ArgvGlob: "make deploy", Action: DecisionAllow}); err != nil {
				t.Fatal(err)
			}
		},
		"earlier wider allow only in the live profile": func(e *Engine, _ string) {
			e.profileRules = []Rule{{Tool: "Bash", Action: DecisionAllow}}
		},
		"wider allow only in the saved preset": func(_ *Engine, presetPath string) {
			if err := SavePreset(presetPath, &PresetFile{Rules: []Rule{{Tool: "*", Action: DecisionAllow}}}); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			presetPath := filepath.Join(t.TempDir(), "preset.json")
			e := New(nil)
			setup(e, presetPath)
			sess := ids.NewSessionID()
			e.AddSessionRule(sess, deny)
			if _, err := e.Promote(presetPath, sess, 0); err == nil {
				t.Fatal("Promote dropped a session deny that a wider allow would override")
			}
			if got := e.ListSessionRules(sess); len(got) != 1 || got[0] != deny {
				t.Fatalf("session rules after refused promotion = %+v", got)
			}
			if d := e.Evaluate(sess, "Bash", "make deploy", true); d != DecisionDeny {
				t.Fatalf("Evaluate after refused promotion = %v, want deny", d)
			}
			if pf, _ := LoadPreset(presetPath); slices.Contains(pf.Rules, deny) {
				t.Fatalf("refused promotion still wrote the preset: %+v", pf.Rules)
			}
		})
	}
}
