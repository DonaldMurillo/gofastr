package check

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Sanity-check the inline-script linter, uses a temp dir with
// controlled fixture .go files so we never depend on the rest of
// the repo to define what's "bad".

func writeFixture(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// requireNoInlineScripts writes one fixture file into a fresh dir, lints
// the dir, and fails the test unless the result is clean.
func requireNoInlineScripts(t *testing.T, name, body string) {
	t.Helper()
	dir := t.TempDir()
	writeFixture(t, dir, name, body)
	res, err := LintNoInlineScripts(dir)
	if err != nil {
		t.Fatal(err)
	}
	if res.HasErrors() {
		t.Errorf("expected no violations, got:\n%s", res.Error())
	}
}

func TestLintNoInlineScripts_FlagsInlineBlock(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "bad.go", `package x
import "github.com/DonaldMurillo/gofastr/core/render"
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
	requireNoInlineScripts(t, "ok.go", `package x
import "github.com/DonaldMurillo/gofastr/core/render"
var x = render.HTML(`+"`<script src=\"/x.js\"></script>`"+`)
`)
}

func TestLintNoInlineScripts_FlagsTagCallWithChildren(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "bad.go", `package x
import "github.com/DonaldMurillo/gofastr/core/render"
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
	requireNoInlineScripts(t, "ok.go", `package x
import "github.com/DonaldMurillo/gofastr/core/render"
var _ = render.Tag("script", map[string]string{"src": "/x.js"})
`)
}

func TestLintNoInlineScripts_AllowsInertJSONType(t *testing.T) {
	requireNoInlineScripts(t, "ok.go", `package x
import "github.com/DonaldMurillo/gofastr/core/render"
var _ = render.Tag("script", map[string]string{"type": "application/json"}, render.HTML("{\"x\":1}"))
`)
}

func TestLintNoInlineScripts_AllowsInertJSONLiteral(t *testing.T) {
	requireNoInlineScripts(t, "ok.go", `package x
import "github.com/DonaldMurillo/gofastr/core/render"
var _ = render.HTML(`+"`<script type=\"application/json\">{\"x\":1}</script>`"+`)
`)
}

func TestLintNoInlineScripts_FlagsExecutableType(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "bad.go", `package x
import "github.com/DonaldMurillo/gofastr/core/render"
var _ = render.Tag("script", map[string]string{"type": "text/javascript"}, render.HTML("alert(1)"))
`)
	res, err := LintNoInlineScripts(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !res.HasErrors() {
		t.Fatal("expected violation: text/javascript with body is executable inline JS")
	}
}

func TestLintNoInlineScripts_SkipsTestFiles(t *testing.T) {
	requireNoInlineScripts(t, "bad_test.go", `package x
import "github.com/DonaldMurillo/gofastr/core/render"
var _ = render.HTML(`+"`<script>alert(1)</script>`"+`)
`)
}

func TestLintNoInlineScripts_HonorsIgnoreDirective(t *testing.T) {
	requireNoInlineScripts(t, "bad.go", `//check-csp:ignore-file
package x
import "github.com/DonaldMurillo/gofastr/core/render"
var _ = render.HTML(`+"`<script>alert(1)</script>`"+`)
`)
}

func TestLintNoInlineScripts_HonorsGofmtSpacedDirective(t *testing.T) {
	// gofmt rewrites //check-csp:… to "// check-csp:…" (hyphenated names
	// don't qualify as Go directives), so the spaced form must work too.
	requireNoInlineScripts(t, "bad.go", `// check-csp:ignore-file
package x
import "github.com/DonaldMurillo/gofastr/core/render"
var _ = render.HTML(`+"`<script>alert(1)</script>`"+`)
`)
}

func TestCSPIgnoreDirectiveCRLF(t *testing.T) {
	for _, raw := range []string{"//check-csp:ignore-file\r\npackage x\r\n", "// check-csp:ignore-file\r\npackage x\r\n"} {
		if !hasCSPIgnoreDirective([]byte(raw)) {
			t.Fatalf("CRLF directive was not recognized: %q", raw)
		}
	}
	if hasCSPIgnoreDirective([]byte("// check-csp:ignore-file\r trailing text\n")) {
		t.Fatal("embedded carriage return must not hide trailing text")
	}
}

func TestLintNoInlineScripts_ReportsLineNumber(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "bad.go", `package x
import "github.com/DonaldMurillo/gofastr/core/render"
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
//
//gofastr:test-reads *.go
func TestLintNoInlineScripts_RepoIsClean(t *testing.T) {
	root := repoRoot(t)
	res, err := LintNoInlineScriptsRecursive(root)
	if err != nil {
		t.Fatal(err)
	}
	if res.HasErrors() {
		t.Errorf("repo contains inline-<script> violations — `make build` would refuse to compile:\n%s",
			strings.TrimSpace(res.Error()))
	}
}

// repoRoot locates the repository root by walking up from this file until
// we find go.mod. It avoids hard-coded relative paths fragile to refactors
// and skips when the root cannot be found.
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := wd
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			return root
		}
		parent := filepath.Dir(root)
		if parent == root {
			t.Skip("could not locate repo root")
		}
		root = parent
	}
}
