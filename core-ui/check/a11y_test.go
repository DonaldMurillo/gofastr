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

func TestLintA11yFlagsMissingAlt(t *testing.T) {
	path := writeA11yFixture(t, `package app

import "github.com/DonaldMurillo/gofastr/core-ui/html"

func view() any {
	return html.Image(html.ImageConfig{Src: "/x.png"})
}
`)
	res, err := LintA11yFile(path)
	if err != nil {
		t.Fatalf("LintA11yFile: %v", err)
	}
	if len(res.Violations) != 1 {
		t.Fatalf("expected 1 violation, got %d: %s", len(res.Violations), res.Error())
	}
	if !strings.Contains(res.Violations[0].Message, `"Alt"`) {
		t.Errorf("violation should name Alt, got %q", res.Violations[0].Message)
	}
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
		t.Fatalf("aliased import must be linted, got %d: %s", len(res.Violations), res.Error())
	}
	if !strings.Contains(res.Violations[0].Message, `"Label"`) {
		t.Errorf("violation should name Label, got %q", res.Violations[0].Message)
	}
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
