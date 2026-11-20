package check

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeA11yFixture(t *testing.T, src string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "screen.go")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// lintA11yNaming lints src and asserts exactly one violation naming
// want (the missing a11y field).
func lintA11yNaming(t *testing.T, src, want string) {
	t.Helper()
	res, err := LintA11yFile(writeA11yFixture(t, src))
	if err != nil {
		t.Fatalf("LintA11yFile: %v", err)
	}
	if len(res.Violations) != 1 {
		t.Fatalf("expected 1 violation, got %d: %s", len(res.Violations), res.Error())
	}
	if !strings.Contains(res.Violations[0].Message, want) {
		t.Errorf("violation should name %s, got %q", want, res.Violations[0].Message)
	}
}

func TestLintA11yFlagsMissingAlt(t *testing.T) {
	lintA11yNaming(t, `package app

import "github.com/DonaldMurillo/gofastr/core-ui/html"

func view() any {
	return html.Image(html.ImageConfig{Src: "/x.png"})
}
`, `"Alt"`)
}

func TestLintA11ySkipsNonHTMLPackages(t *testing.T) {
	path := writeA11yFixture(t, `package app

import "example.com/other/html"

func view() any {
	// Same function name, different package — NOT the core-ui/html API.
	return html.Image(html.ImageConfig{Src: "/x.png"})
}
`)
	res, err := LintA11yFile(path)
	if err != nil {
		t.Fatalf("LintA11yFile: %v", err)
	}
	if len(res.Violations) != 0 {
		t.Errorf("foreign html package must not be linted, got: %s", res.Error())
	}
}

func TestLintA11ySkipsLocalFunctions(t *testing.T) {
	path := writeA11yFixture(t, `package app

type ButtonConfig struct{ Kind string }

func Button(c ButtonConfig) any { return nil }

func view() any {
	return Button(ButtonConfig{Kind: "primary"})
}
`)
	res, err := LintA11yFile(path)
	if err != nil {
		t.Fatalf("LintA11yFile: %v", err)
	}
	if len(res.Violations) != 0 {
		t.Errorf("local same-name function must not be linted, got: %s", res.Error())
	}
}

func TestLintA11yAliasedImport(t *testing.T) {
	lintA11yNaming(t, `package app

import h "github.com/DonaldMurillo/gofastr/core-ui/html"

func view() any {
	return h.Button(h.ButtonConfig{})
}
`, `"Label"`)
}

func TestLintA11yOrFieldsSatisfiedByEither(t *testing.T) {
	path := writeA11yFixture(t, `package app

import "github.com/DonaldMurillo/gofastr/core-ui/html"

func view() any {
	return html.Nav(html.NavConfig{LabelledBy: "site-nav-heading"})
}
`)
	res, err := LintA11yFile(path)
	if err != nil {
		t.Fatalf("LintA11yFile: %v", err)
	}
	if len(res.Violations) != 0 {
		t.Errorf("LabelledBy satisfies the Nav rule, got: %s", res.Error())
	}
}

func TestLintA11ySkipsShadowedAlias(t *testing.T) {
	path := writeA11yFixture(t, `package app

import "github.com/DonaldMurillo/gofastr/core-ui/html"

type builder struct{}
type btnOpts struct{ Kind string }

func (builder) Button(o btnOpts) any { return nil }

func real() any {
	return html.Div(html.DivConfig{})
}

func shadowed() any {
	html := builder{}
	return html.Button(btnOpts{Kind: "primary"})
}
`)
	res, err := LintA11yFile(path)
	if err != nil {
		t.Fatalf("LintA11yFile: %v", err)
	}
	if len(res.Violations) != 0 {
		t.Errorf("shadowed alias must not be linted, got: %s", res.Error())
	}
}

func TestLintA11yStillFlagsQualifiedConfig(t *testing.T) {
	path := writeA11yFixture(t, `package app

import h "github.com/DonaldMurillo/gofastr/core-ui/html"

func view() any {
	return h.Button(h.ButtonConfig{})
}
`)
	res, err := LintA11yFile(path)
	if err != nil {
		t.Fatalf("LintA11yFile: %v", err)
	}
	if len(res.Violations) != 1 {
		t.Fatalf("qualified config literal must still be flagged, got %d: %s", len(res.Violations), res.Error())
	}
}

func TestLintA11yAcceptsIconOnlyButtonAriaLabel(t *testing.T) {
	path := writeA11yFixture(t, `package app

import "github.com/DonaldMurillo/gofastr/core-ui/html"

func view() any {
	// The supported icon-only form — html.Button's runtime contract
	// accepts aria-label in place of a visible Label.
	return html.Button(html.ButtonConfig{ExtraAttrs: html.Attrs{"aria-label": "Close"}})
}
`)
	res, err := LintA11yFile(path)
	if err != nil {
		t.Fatalf("LintA11yFile: %v", err)
	}
	if len(res.Violations) != 0 {
		t.Errorf("icon-only button with aria-label must pass, got: %s", res.Error())
	}
}

func TestLintA11yOpaqueExtraAttrsFailsOpen(t *testing.T) {
	path := writeA11yFixture(t, `package app

import "github.com/DonaldMurillo/gofastr/core-ui/html"

func view(attrs html.Attrs) any {
	// ExtraAttrs from a variable can't be inspected statically; the
	// runtime panic backstops a missing aria-label, so the lint must
	// not block the build on it.
	return html.Button(html.ButtonConfig{ExtraAttrs: attrs})
}
`)
	res, err := LintA11yFile(path)
	if err != nil {
		t.Fatalf("LintA11yFile: %v", err)
	}
	if len(res.Violations) != 0 {
		t.Errorf("opaque ExtraAttrs must fail open, got: %s", res.Error())
	}
}

func TestLintA11yExtraAttrsWithoutAriaLabelStillFlags(t *testing.T) {
	path := writeA11yFixture(t, `package app

import "github.com/DonaldMurillo/gofastr/core-ui/html"

func view() any {
	return html.Button(html.ButtonConfig{ExtraAttrs: html.Attrs{"data-x": "1"}})
}
`)
	res, err := LintA11yFile(path)
	if err != nil {
		t.Fatalf("LintA11yFile: %v", err)
	}
	if len(res.Violations) != 1 {
		t.Errorf("ExtraAttrs literal without aria-label must still flag, got %d: %s", len(res.Violations), res.Error())
	}
}

func TestLintA11yNavAriaLabelViaExtraAttrs(t *testing.T) {
	path := writeA11yFixture(t, `package app

import "github.com/DonaldMurillo/gofastr/core-ui/html"

func view() any {
	return html.Nav(html.NavConfig{ExtraAttrs: html.Attrs{"aria-label": "Footer"}})
}
`)
	res, err := LintA11yFile(path)
	if err != nil {
		t.Fatalf("LintA11yFile: %v", err)
	}
	if len(res.Violations) != 0 {
		t.Errorf("Nav with aria-label via ExtraAttrs must pass, got: %s", res.Error())
	}
}
