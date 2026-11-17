package contracts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSweepConfigStandaloneDirectiveCoversItsLineAndTheNextCode(t *testing.T) {
	_, sup := probePass(t, map[string]string{
		"a.go": "package a\n\n//gofastr:allow(GOFASTR1403, GOFASTR1003) reviewed\n\nfunc g() { trip() }\nfunc h() { trip() }\n",
	})
	html, _ := LookupRule(RuleHTMLConcat)
	untested, _ := LookupRule(RuleUntestedRoute)
	for _, c := range []struct {
		rule Rule
		line int
		want bool
	}{{html, 3, true}, {html, 5, true}, {untested, 5, true}, {html, 6, false}} {
		if got := sup.suppressed(Diagnostic{File: "a.go", Line: c.line}, c.rule); got != c.want {
			t.Errorf("%s at line %d: suppressed = %v, want %v", c.rule.ID, c.line, got, c.want)
		}
	}
}

func TestSweepConfigUnknownSeverityIsAnError(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "gofastr.settings.yml", "contracts:\n  rules:\n    GOFASTR1003: warnn\n")
	if _, err := LoadConfig(dir, ""); err == nil || !strings.Contains(err.Error(), "unknown severity") {
		t.Fatalf("a typo'd severity loaded: %v", err)
	}
}

func TestSweepConfigRuleOverrideBeatsCapabilityOverride(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "gofastr.settings.yml", "contracts:\n  security: \"off\"\n  rules:\n    GOFASTR1403: warn\n")
	cfg, err := LoadConfig(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	html, _ := LookupRule(RuleHTMLConcat)
	if got := cfg.SeverityFor(html); got != SeverityWarn {
		t.Errorf("rule override lost to the capability override: %v", got)
	}
	other := RulesFor(CapSecurity)
	for _, r := range other {
		if r.ID != html.ID && cfg.Enabled(r) {
			t.Errorf("capability off did not reach %s", r.ID)
		}
	}
}

func TestSweepConfigDirectiveInsideARawStringIsNotLive(t *testing.T) {
	_, sup := probePass(t, map[string]string{
		"a.go": "package a\n\nconst doc = `\n//gofastr:allow(GOFASTR1403) documented, not live\n`\n\nfunc f() { trip() }\n",
	})
	html, _ := LookupRule(RuleHTMLConcat)
	for line := 1; line <= 7; line++ {
		if sup.suppressed(Diagnostic{File: "a.go", Line: line}, html) {
			t.Fatalf("line %d suppressed by a directive inside a string", line)
		}
	}
}

func TestSweepConfigUnknownKeyIsAnError(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "gofastr.settings.yml", "contracts:\n  stirct: true\n")
	if _, err := LoadConfig(dir, ""); err == nil || !strings.Contains(err.Error(), "unknown contracts key") {
		t.Fatalf("a misspelt key loaded: %v", err)
	}
}

func TestSweepConfigDefaultDemandsEveryCoverageKind(t *testing.T) {
	c := DefaultConfig().Coverage
	if !c.Routes || !c.Permissions || !c.Entities || c.MinimumSet {
		t.Errorf("default coverage is not strict: %+v", c)
	}
}

func sweepGitRepo(t *testing.T) (string, func(...string)) {
	t.Helper()
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main", ".")
	git("config", "user.email", "t@example.com")
	git("config", "user.name", "t")
	writeTemp(t, dir, "base.go", "package a\n")
	git("add", "-A")
	git("commit", "-qm", "base")
	return dir, git
}

func TestSweepConfigChangedComparesFromTheMergeBase(t *testing.T) {
	dir, git := sweepGitRepo(t)
	git("checkout", "-qb", "topic")
	writeTemp(t, dir, "mine.go", "package a\n")
	git("add", "-A")
	git("commit", "-qm", "mine")
	git("checkout", "-q", "main")
	writeTemp(t, dir, "base.go", "package a\n\nvar Theirs = 1\n")
	git("add", "-A")
	git("commit", "-qm", "theirs")
	git("checkout", "-q", "topic")
	files, err := ChangedFiles(dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	if !files["mine.go"] || files["base.go"] {
		t.Errorf("changed set = %v, want this branch's mine.go only", SortedFiles(files))
	}
}

func TestSweepConfigChangedResolvesASymlinkedRoot(t *testing.T) {
	dir, git := sweepGitRepo(t)
	writeTemp(t, dir, "app/x.go", "package app\n")
	git("add", "-A")
	git("commit", "-qm", "app")
	writeTemp(t, dir, "app/x.go", "package app\n\nvar X = 1\n")
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	files, err := ChangedFiles(filepath.Join(link, "app"), "")
	if err != nil {
		t.Fatal(err)
	}
	if !files["x.go"] {
		t.Errorf("changed set = %v, want x.go relative to the symlinked root", SortedFiles(files))
	}
}
