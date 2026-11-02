package auditcmd

import (
	"path/filepath"
	"strings"
	"testing"
)

// auditDeps scans every .go file under root for calls to the init-time
// global-registration APIs that a malicious dependency could abuse
// (style.Contribute, render.RegisterComponent, etc.) and returns one
// AuditFinding per call site grouped by import path. The test fixtures
// below pin the shape that the gofastr CLI surface relies on.

func TestAuditDeps_FindsStyleContributeCallsByPackage(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, "go.mod", "module example.com/audit-test\ngo 1.22\n")
	mustWrite(t, dir, "alpha/alpha.go", `package alpha

import "github.com/DonaldMurillo/gofastr/core-ui/style"

var _ = style.Contribute(func(ss *style.StyleSheet) {
	ss.Rule(".alpha").Set("color", "red").End()
})
`)
	mustWrite(t, dir, "beta/beta.go", `package beta

import "github.com/DonaldMurillo/gofastr/core-ui/style"

var _ = style.Contribute(func(ss *style.StyleSheet) {
	ss.Rule(".beta").Set("color", "blue").End()
})

var _ = style.Contribute(func(ss *style.StyleSheet) {
	ss.Rule(".beta2").Set("color", "green").End()
})
`)
	mustWrite(t, dir, "boring/boring.go", `package boring

func Add(a, b int) int { return a + b }
`)

	findings, err := auditDeps(dir)
	if err != nil {
		t.Fatal(err)
	}

	got := summariseByKind(findings, "style.Contribute")
	if got["example.com/audit-test/alpha"] != 1 {
		t.Errorf("alpha: want 1 style.Contribute, got %d", got["example.com/audit-test/alpha"])
	}
	if got["example.com/audit-test/beta"] != 2 {
		t.Errorf("beta: want 2 style.Contribute, got %d", got["example.com/audit-test/beta"])
	}
	if _, ok := got["example.com/audit-test/boring"]; ok {
		t.Error("boring package should not appear (no init-time registrations)")
	}
}

func TestAuditDeps_DetectsAllTrackedKinds(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, "go.mod", "module example.com/audit-test\ngo 1.22\n")
	mustWrite(t, dir, "multi/multi.go", `package multi

import (
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

var _ = style.Contribute(func(ss *style.StyleSheet) {})

func init() {
	render.RegisterComponent("x", func() {})
	render.RegisterLayout("y", func() {})
	render.RegisterFunc("z", func() {})
}
`)

	findings, err := auditDeps(dir)
	if err != nil {
		t.Fatal(err)
	}

	kinds := make(map[string]bool)
	for _, f := range findings {
		if f.Pkg == "example.com/audit-test/multi" {
			kinds[f.Kind] = true
		}
	}
	for _, want := range []string{"style.Contribute", "render.RegisterComponent", "render.RegisterLayout", "render.RegisterFunc"} {
		if !kinds[want] {
			t.Errorf("missing kind %q in findings: %+v", want, findings)
		}
	}
}

// The scan matched a call only when the file imported the package under
// its usual name, so a renamed import (cuistyle.Contribute) or a dot
// import (bare Contribute) hid the registration from the audit whose job
// is to surface it.
func TestAuditDeps_FindsRenamedAndDotImports(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, "go.mod", "module example.com/audit-test\ngo 1.22\n")
	mustWrite(t, dir, "renamed/renamed.go", `package renamed

import (
	cuistyle "github.com/DonaldMurillo/gofastr/core-ui/style"
	r "github.com/DonaldMurillo/gofastr/core/render"
)

var _ = cuistyle.Contribute(func(ss *cuistyle.StyleSheet) {})

func init() { r.RegisterComponent("x", func() {}) }
`)
	mustWrite(t, dir, "dotted/dotted.go", `package dotted

import . "github.com/DonaldMurillo/gofastr/core-ui/style"

var _ = Contribute(func(ss *StyleSheet) {})
`)
	mustWrite(t, dir, "shadow/shadow.go", `package shadow

import style "example.com/not/gofastr/style"

var _ = style.Contribute(nil)
`)

	findings, err := auditDeps(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, f := range findings {
		got[f.Pkg+" "+f.Kind] = true
	}
	for _, want := range []string{
		"example.com/audit-test/renamed style.Contribute",
		"example.com/audit-test/renamed render.RegisterComponent",
		"example.com/audit-test/dotted style.Contribute",
	} {
		if !got[want] {
			t.Errorf("missing %q in findings: %+v", want, findings)
		}
	}
	if got["example.com/audit-test/shadow style.Contribute"] {
		t.Error("a package merely named style is not core-ui/style")
	}
}

func TestAuditDeps_HumanReportListsPackagesAndKinds(t *testing.T) {
	findings := []AuditFinding{
		{Pkg: "example.com/a", Kind: "style.Contribute", File: "a/a.go", Line: 10},
		{Pkg: "example.com/a", Kind: "style.Contribute", File: "a/a.go", Line: 20},
		{Pkg: "example.com/b", Kind: "render.RegisterComponent", File: "b/b.go", Line: 5},
	}
	out := formatAuditReport(findings)
	if !strings.Contains(out, "example.com/a") {
		t.Errorf("report missing package example.com/a:\n%s", out)
	}
	if !strings.Contains(out, "style.Contribute") {
		t.Errorf("report missing kind style.Contribute:\n%s", out)
	}
	// Counts per pkg+kind should appear.
	if !strings.Contains(out, "×2") && !strings.Contains(out, "x2") && !strings.Contains(out, "(2)") {
		t.Errorf("report missing the duplicate count for a/a.go style.Contribute:\n%s", out)
	}
}

func mustWrite(t *testing.T, root, rel, body string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := mkdirAll(filepath.Dir(full)); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(full, []byte(body)); err != nil {
		t.Fatal(err)
	}
}

func summariseByKind(findings []AuditFinding, kind string) map[string]int {
	out := make(map[string]int)
	for _, f := range findings {
		if f.Kind == kind {
			out[f.Pkg]++
		}
	}
	return out
}
