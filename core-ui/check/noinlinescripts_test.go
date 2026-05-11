package check

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Sanity-check the inline-script linter — uses a temp dir with
// controlled fixture .go files so we never depend on the rest of
// the repo to define what's "bad".

func writeFixture(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func TestLintNoInlineScripts_FlagsInlineBlock(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "bad.go", `package x
import "github.com/gofastr/gofastr/core/render"
var x = render.HTML(`+"`<script>alert(1)</script>`"+`)
`)
	res, err := LintNoInlineScripts(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !res.HasErrors() {
		t.Fatal("expected violation for inline <script> block")
	}
}

func TestLintNoInlineScripts_AllowsExternalSrc(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "ok.go", `package x
import "github.com/gofastr/gofastr/core/render"
var x = render.HTML(`+"`<script src=\"/x.js\"></script>`"+`)
`)
	res, err := LintNoInlineScripts(dir)
	if err != nil {
		t.Fatal(err)
	}
	if res.HasErrors() {
		t.Errorf("external <script src=…> must pass:\n%s", res.Error())
	}
}

func TestLintNoInlineScripts_FlagsTagCallWithChildren(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "bad.go", `package x
import "github.com/gofastr/gofastr/core/render"
var _ = render.Tag("script", nil, render.HTML("alert(1)"))
`)
	res, err := LintNoInlineScripts(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !res.HasErrors() {
		t.Fatal("expected violation for render.Tag(\"script\", …) with children")
	}
}

func TestLintNoInlineScripts_AllowsTagCallWithSrc(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "ok.go", `package x
import "github.com/gofastr/gofastr/core/render"
var _ = render.Tag("script", map[string]string{"src": "/x.js"})
`)
	res, err := LintNoInlineScripts(dir)
	if err != nil {
		t.Fatal(err)
	}
	if res.HasErrors() {
		t.Errorf("script tag with src should pass:\n%s", res.Error())
	}
}

func TestLintNoInlineScripts_SkipsTestFiles(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "bad_test.go", `package x
import "github.com/gofastr/gofastr/core/render"
var _ = render.HTML(`+"`<script>alert(1)</script>`"+`)
`)
	res, err := LintNoInlineScripts(dir)
	if err != nil {
		t.Fatal(err)
	}
	if res.HasErrors() {
		t.Errorf("test file violations should be ignored:\n%s", res.Error())
	}
}

func TestLintNoInlineScripts_HonorsIgnoreDirective(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "bad.go", `//check-csp:ignore-file
package x
import "github.com/gofastr/gofastr/core/render"
var _ = render.HTML(`+"`<script>alert(1)</script>`"+`)
`)
	res, err := LintNoInlineScripts(dir)
	if err != nil {
		t.Fatal(err)
	}
	if res.HasErrors() {
		t.Errorf("//check-csp:ignore-file directive must suppress:\n%s", res.Error())
	}
}

func TestLintNoInlineScripts_ReportsLineNumber(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "bad.go", `package x
import "github.com/gofastr/gofastr/core/render"
var _ = render.HTML("ok")
var _ = render.HTML(`+"`<script>alert(1)</script>`"+`)
`)
	res, err := LintNoInlineScripts(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !res.HasErrors() {
		t.Fatal("expected violation")
	}
	if res.Violations[0].Line != 4 {
		t.Errorf("expected violation on line 4, got %d (msg: %s)",
			res.Violations[0].Line, res.Violations[0].Message)
	}
}

func TestLintNoInlineScriptsRecursive_WalksSubdirs(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "deep", "nested")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, sub, "bad.go", `package x
var _ = `+"`<script>alert(1)</script>`"+`
`)
	res, err := LintNoInlineScriptsRecursive(root)
	if err != nil {
		t.Fatal(err)
	}
	if !res.HasErrors() {
		t.Fatal("expected nested violation to surface")
	}
}

// TestLintNoInlineScripts_RepoIsClean is the build-time enforcement:
// run the linter against the live repo root and refuse to pass if
// any production file emits inline <script>. This is the test-suite
// mirror of `make csp-check`; CI runs both.
func TestLintNoInlineScripts_RepoIsClean(t *testing.T) {
	// Locate repo root by walking up from this file until we find
	// go.mod. Avoids hard-coded relative paths fragile to refactors.
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
	res, err := LintNoInlineScriptsRecursive(root)
	if err != nil {
		t.Fatal(err)
	}
	if res.HasErrors() {
		t.Errorf("repo contains inline-<script> violations — `make build` would refuse to compile:\n%s",
			strings.TrimSpace(res.Error()))
	}
}
