package check

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeStyleFixture(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.go")
	if err := os.WriteFile(path, []byte("package fixture\n\n"+body), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLintNoInlineStyles_FlagsRawAttributeInString(t *testing.T) {
	dir := writeStyleFixture(t, `
var x = "<div style=\"color:red\">hi</div>"
`)
	res, err := LintNoInlineStyles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !res.HasErrors() {
		t.Fatal("expected violation for raw style attribute, got none")
	}
	if !strings.Contains(res.Error(), "inline style") {
		t.Errorf("violation message missing expected phrase: %s", res.Error())
	}
}

func TestLintNoInlineStyles_FlagsBackticksLiteral(t *testing.T) {
	dir := writeStyleFixture(t, "var x = `<button style='padding:1rem'>x</button>`")
	res, err := LintNoInlineStyles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !res.HasErrors() {
		t.Fatal("expected violation for backtick literal style attribute")
	}
}

func TestLintNoInlineStyles_FlagsAttrsMapKey(t *testing.T) {
	dir := writeStyleFixture(t, `
import _ "fmt"

var attrs = map[string]string{"style": "display:flex"}
`)
	res, err := LintNoInlineStyles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !res.HasErrors() {
		t.Fatal("expected violation for \"style\" map key")
	}
}

func TestLintNoInlineStyles_AllowsClassAttribute(t *testing.T) {
	dir := writeStyleFixture(t, `
var x = "<div class=\"demo-modal-body\">hi</div>"
var y = map[string]string{"class": "demo-button-row"}
`)
	res, err := LintNoInlineStyles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if res.HasErrors() {
		t.Errorf("class-only fixture should be clean, got: %s", res.Error())
	}
}

func TestLintNoInlineStyles_SkipsTestFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "thing_test.go"),
		[]byte(`package fixture

var x = "<div style=\"x\"></div>"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := LintNoInlineStyles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if res.HasErrors() {
		t.Errorf("_test.go files should be skipped, got: %s", res.Error())
	}
}

func TestLintNoInlineStyles_HonorsIgnoreDirective(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "fixture.go"),
		[]byte(`//check-csp:ignore-file
package fixture

var x = "<div style=\"x\"></div>"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := LintNoInlineStyles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if res.HasErrors() {
		t.Errorf("file-level ignore-directive should suppress, got: %s", res.Error())
	}
}

// TestLintNoInlineStyles_RepoIsClean mirrors the script linter's
// repo-clean test: run the inline-style check against the live tree
// and fail if any production file emits an inline style attribute.
// CI runs both.
func TestLintNoInlineStyles_RepoIsClean(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := wd
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			t.Skip("could not locate repo root")
		}
		root = parent
	}
	res, err := LintNoInlineStylesRecursive(root)
	if err != nil {
		t.Fatal(err)
	}
	if res.HasErrors() {
		t.Errorf("repo contains inline style=\"…\" violations — `check-csp` would fail:\n%s",
			strings.TrimSpace(res.Error()))
	}
}

func TestLintNoInlineStyles_DoesNotFlagWordContainingStyle(t *testing.T) {
	dir := writeStyleFixture(t, `
var x = "Refer to lifestyle-section in the docs"
var y = "this stylesheet is fine"
`)
	res, err := LintNoInlineStyles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if res.HasErrors() {
		t.Errorf("words containing 'style' substring should not match: %s", res.Error())
	}
}
