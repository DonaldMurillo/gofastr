package check

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLintNoVarJS_FlagsBareVar pins the basic positive case:
// a top-level `var x = …` declaration is flagged.
func TestLintNoVarJS_FlagsBareVar(t *testing.T) {
	dir := t.TempDir()
	writeJS(t, dir, "bad.js", `'use strict';
var x = 1;
const y = 2;`)
	res, err := LintNoVarJS(dir)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(res.Violations) != 1 {
		t.Fatalf("want 1 violation, got %d: %v", len(res.Violations), res.Violations)
	}
	if res.Violations[0].Line != 2 {
		t.Errorf("expected line 2, got %d", res.Violations[0].Line)
	}
}

// TestLintNoVarJS_IgnoresIdentifierLookalikes guards against the
// obvious false-positive surface: `vary`, `variety`, `myvar`, `varX`
// must NOT trip the check. The keyword needs word boundaries.
func TestLintNoVarJS_IgnoresIdentifierLookalikes(t *testing.T) {
	dir := t.TempDir()
	writeJS(t, dir, "ok.js", `'use strict';
const vary = 1;
const variety = 2;
const myvar = 3;
const varX = 4;`)
	res, err := LintNoVarJS(dir)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(res.Violations) != 0 {
		t.Errorf("expected zero violations, got: %v", res.Violations)
	}
}

// TestLintNoVarJS_IgnoresStringsAndComments pins the comment/string
// sanitizer: the literal word "var" inside a string or comment must
// not produce a violation.
func TestLintNoVarJS_IgnoresStringsAndComments(t *testing.T) {
	dir := t.TempDir()
	writeJS(t, dir, "comments.js", `'use strict';
// var x = 1;
/* var y = 2; */
const a = 'var z = 3';
const b = "var z = 4";
const c = ` + "`var z = 5`" + `;`)
	res, err := LintNoVarJS(dir)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(res.Violations) != 0 {
		t.Errorf("expected zero violations, got: %v", res.Violations)
	}
}

// TestLintNoVarJS_HonorsIgnoreDirective guards the escape-hatch: a
// file with //check-novar:ignore-file is skipped.
func TestLintNoVarJS_HonorsIgnoreDirective(t *testing.T) {
	dir := t.TempDir()
	writeJS(t, dir, "legacy.js", `//check-novar:ignore-file
'use strict';
var x = 1;`)
	res, err := LintNoVarJS(dir)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(res.Violations) != 0 {
		t.Errorf("ignore-file directive should suppress violations, got: %v", res.Violations)
	}
}

// TestLintNoVarJS_FlagsMidLineVar covers the case where `var` doesn't
// start the line: e.g. `for (var i = 0; ...)`.
func TestLintNoVarJS_FlagsMidLineVar(t *testing.T) {
	dir := t.TempDir()
	writeJS(t, dir, "loop.js", `for (var i = 0; i < 10; i++) console.log(i);`)
	res, err := LintNoVarJS(dir)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(res.Violations) != 1 {
		t.Fatalf("want 1 violation, got %d: %v", len(res.Violations), res.Violations)
	}
}

// TestLintNoVarJS_RepoIsClean asserts the production runtime modules
// (core-ui/runtime/*.js + core-ui/runtime/src/*.js) ship without any
// var declarations. Mirrors the existing TestLintNoInlineScripts_RepoIsClean
// pattern — if anyone reintroduces `var`, `go test ./...` flags it.
func TestLintNoVarJS_RepoIsClean(t *testing.T) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Skipf("can't locate repo root: %v", err)
	}
	runtimeDir := filepath.Join(repoRoot, "core-ui", "runtime")
	if _, err := os.Stat(runtimeDir); err != nil {
		t.Skipf("runtime dir not present: %v", err)
	}
	res, err := LintNoVarJSRecursive(runtimeDir)
	if err != nil {
		t.Fatalf("lint: %v", err)
	}
	if res.HasErrors() {
		t.Errorf("runtime JS contains `var` declarations:\n%s", res.Error())
	}
}

func writeJS(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// findRepoRoot walks up from the test's working directory looking for
// the go.mod that declares the gofastr module. Used by RepoIsClean to
// resolve the absolute path of core-ui/runtime independent of where
// the test was invoked from.
func findRepoRoot() (string, error) {
	cur, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if data, err := os.ReadFile(filepath.Join(cur, "go.mod")); err == nil {
			if strings.Contains(string(data), "module github.com/DonaldMurillo/gofastr") {
				return cur, nil
			}
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", os.ErrNotExist
		}
		cur = parent
	}
}
