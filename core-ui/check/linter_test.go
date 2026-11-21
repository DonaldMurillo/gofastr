package check

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTempGoFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

// lintFixture lints a temp .ui.go file holding content and fails the
// test on an unexpected error.
func lintFixture(t *testing.T, name, content string) *Result {
	t.Helper()
	path := writeTempGoFile(t, t.TempDir(), name, content)
	result, err := LintFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return result
}

// requireCleanLint lints a temp .ui.go file holding content and fails
// the test unless it reports no violations.
func requireCleanLint(t *testing.T, name, content string) {
	t.Helper()
	result := lintFixture(t, name, content)
	if result.HasErrors() {
		t.Errorf("expected no violations, got:\n%s", result.Error())
	}
}

// requireViolation fails unless result reports a violation whose message
// is exactly want.
func requireViolation(t *testing.T, result *Result, want string) {
	t.Helper()
	for _, v := range result.Violations {
		if v.Message == want {
			return
		}
	}
	t.Errorf("expected violation %q, got: %v", want, result.Violations)
}

// requireViolationContaining fails unless result reports a violation
// whose message contains want.
func requireViolationContaining(t *testing.T, result *Result, want string) {
	t.Helper()
	for _, v := range result.Violations {
		if strings.Contains(v.Message, want) {
			return
		}
	}
	t.Errorf("expected a violation containing %q, got: %v", want, result.Violations)
}

func TestLintValidFile(t *testing.T) {
	requireCleanLint(t, "valid.ui.go", `package test
import "fmt"
import "github.com/DonaldMurillo/gofastr/core-ui/html"
func Render() string { return fmt.Sprintf("hello") }
`)
}

func TestLintGoroutine(t *testing.T) {
	result := lintFixture(t, "goroutine.ui.go", `package test
func Render() { go func() {}() }
`)
	requireViolation(t, result, "goroutines not allowed in .ui.go files")
}

func TestLintChannel(t *testing.T) {
	dir := t.TempDir()
	content := `package test
func Bad() {
	ch := make(chan int)
	ch <- 42
	_ = <-ch
}
`
	path := writeTempGoFile(t, dir, "channel.ui.go", content)
	result, err := LintFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.HasErrors() {
		t.Fatal("expected violations for channel operations, got none")
	}
	// Expect 3 distinct violations: make(chan), send, receive
	foundMake, foundSend, foundRecv := false, false, false
	for _, v := range result.Violations {
		switch v.Message {
		case "channel creation (make(chan)) not allowed in .ui.go files":
			foundMake = true
		case "channel sends not allowed in .ui.go files":
			foundSend = true
		case "channel receives not allowed in .ui.go files":
			foundRecv = true
		}
	}
	if !foundMake {
		t.Error("expected make(chan) violation")
	}
	if !foundSend {
		t.Error("expected channel send violation")
	}
	if !foundRecv {
		t.Error("expected channel receive violation")
	}
}

func TestLintForbiddenImport(t *testing.T) {
	result := lintFixture(t, "os_import.ui.go", `package test
import "os"
func Bad() { _ = os.Args }
`)
	requireViolation(t, result, "os package not allowed in .ui.go files")
}

func TestLintReflectImport(t *testing.T) {
	result := lintFixture(t, "reflect.ui.go", `package test
import "reflect"
func Bad() { reflect.TypeOf(42) }
`)
	requireViolation(t, result, "reflect package not allowed in .ui.go files")
}

func TestLintAllowedImports(t *testing.T) {
	requireCleanLint(t, "allowed.ui.go", `package test
import (
	"fmt"
	"strings"
	"strconv"
	"html/template"
)
func Ok() {
	_ = fmt.Sprintf("hi")
	_ = strings.ToUpper("hi")
	_ = strconv.Itoa(42)
	_ = template.HTMLEscapeString("<b>")
}
`)
}

func TestLintFrameworkImports(t *testing.T) {
	requireCleanLint(t, "framework.ui.go", `package test
import "github.com/DonaldMurillo/gofastr/core-ui/html"
import "github.com/DonaldMurillo/gofastr/core-ui/component"
func Ok() { _ = html.Div() }
`)
}

func TestLintForLoop(t *testing.T) {
	requireCleanLint(t, "forloop.ui.go", `package test
func Ok() {
	for i := 0; i < 10; i++ { _ = i }
	for _, v := range []string{"a", "b"} { _ = v }
}
`)
}

func TestLintIfElse(t *testing.T) {
	requireCleanLint(t, "ifelse.ui.go", `package test
func Ok(x int) {
	if x > 0 {
		_ = x
	} else {
		_ = -x
	}
}
`)
}

func TestLintMakeChan(t *testing.T) {
	result := lintFixture(t, "makechan.ui.go", `package test
func Bad() { _ = make(chan int) }
`)
	requireViolationContaining(t, result, "channel creation (make(chan))")
}

func TestLintPackage(t *testing.T) {
	dir := t.TempDir()

	writeTempGoFile(t, dir, "good.ui.go", `package test
import "fmt"
func Ok() { fmt.Println("ok") }
`)

	writeTempGoFile(t, dir, "bad.ui.go", `package test
import "os"
func Bad() { _ = os.Args }
`)

	result, err := LintPackage(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.HasErrors() {
		t.Fatal("expected violations from package linting")
	}
}

func TestResultHasErrors(t *testing.T) {
	r := &Result{}
	if r.HasErrors() {
		t.Error("empty result should not have errors")
	}
	r.Violations = append(r.Violations, Violation{File: "test.go", Line: 1, Message: "bad"})
	if !r.HasErrors() {
		t.Error("result with violations should have errors")
	}
}

func TestResultError(t *testing.T) {
	r := &Result{}
	if r.Error() != "" {
		t.Errorf("empty result Error() should be empty, got %q", r.Error())
	}

	r.Violations = append(r.Violations,
		Violation{File: "foo.ui.go", Line: 10, Message: "goroutines not allowed in .ui.go files"},
		Violation{File: "bar.ui.go", Line: 20, Message: "os package not allowed in .ui.go files"},
	)

	got := r.Error()
	if !strings.Contains(got, "foo.ui.go:10: goroutines not allowed in .ui.go files") {
		t.Errorf("Error() missing first violation in:\n%s", got)
	}
	if !strings.Contains(got, "bar.ui.go:20: os package not allowed in .ui.go files") {
		t.Errorf("Error() missing second violation in:\n%s", got)
	}
}

func TestLintNetHTTP(t *testing.T) {
	result := lintFixture(t, "nethttp.ui.go", `package test
import "net/http"
func Bad() { http.Get("http://example.com") }
`)
	requireViolation(t, result, "net/http package not allowed in .ui.go files")
}

func TestLintContextImport(t *testing.T) {
	result := lintFixture(t, "context.ui.go", `package test
import "context"
func Bad() { _ = context.Background() }
`)
	requireViolation(t, result, "context package not allowed in .ui.go files")
}

// ===========================================================================
// Element config required field tests
// ===========================================================================

func TestLintNavMissingLabel(t *testing.T) {
	dir := t.TempDir()
	content := `package test
import "github.com/DonaldMurillo/gofastr/core-ui/html"
func Bad() {
	html.Nav(html.NavConfig{Class: "main"})
}
`
	path := writeTempGoFile(t, dir, "nav_no_label.ui.go", content)
	result, err := LintFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.HasErrors() {
		t.Fatal("expected violation for Nav without Label/LabelledBy")
	}
	found := false
	for _, v := range result.Violations {
		if strings.Contains(v.Message, "Nav") && strings.Contains(v.Message, "Label") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected Nav missing Label violation, got: %v", result.Violations)
	}
}

func TestLintNavWithLabel(t *testing.T) {
	requireCleanLint(t, "nav_with_label.ui.go", `package test
import "github.com/DonaldMurillo/gofastr/core-ui/html"
func Ok() {
	html.Nav(html.NavConfig{Label: "Main"})
}
`)
}

func TestLintSectionMissingLabel(t *testing.T) {
	dir := t.TempDir()
	content := `package test
import "github.com/DonaldMurillo/gofastr/core-ui/html"
func Bad() {
	html.Section(html.SectionConfig{Class: "intro"})
}
`
	path := writeTempGoFile(t, dir, "section_no_label.ui.go", content)
	result, err := LintFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.HasErrors() {
		t.Fatal("expected violation for Section without Label/LabelledBy")
	}
}

func TestLintSectionWithLabelledBy(t *testing.T) {
	requireCleanLint(t, "section_labelledby.ui.go", `package test
import "github.com/DonaldMurillo/gofastr/core-ui/html"
func Ok() {
	html.Section(html.SectionConfig{LabelledBy: "heading-1"})
}
`)
}

func TestLintHeadingMissingLevel(t *testing.T) {
	dir := t.TempDir()
	content := `package test
import "github.com/DonaldMurillo/gofastr/core-ui/html"
func Bad() {
	html.Heading(html.HeadingConfig{Class: "title"})
}
`
	path := writeTempGoFile(t, dir, "heading_no_level.ui.go", content)
	result, err := LintFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.HasErrors() {
		t.Fatal("expected violation for Heading without Level")
	}
}

func TestLintHeadingWithLevel(t *testing.T) {
	requireCleanLint(t, "heading_with_level.ui.go", `package test
import "github.com/DonaldMurillo/gofastr/core-ui/html"
func Ok() {
	html.Heading(html.HeadingConfig{Level: 2})
}
`)
}

func TestLintButtonMissingLabel(t *testing.T) {
	dir := t.TempDir()
	content := `package test
import "github.com/DonaldMurillo/gofastr/core-ui/html"
func Bad() {
	html.Button(html.ButtonConfig{Class: "btn"})
}
`
	path := writeTempGoFile(t, dir, "button_no_label.ui.go", content)
	result, err := LintFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.HasErrors() {
		t.Fatal("expected violation for Button without Label")
	}
}

func TestLintLinkMissingHref(t *testing.T) {
	result := lintFixture(t, "link_no_href.ui.go", `package test
import "github.com/DonaldMurillo/gofastr/core-ui/html"
func Bad() {
	html.Link(html.LinkConfig{Text: "Click"})
}
`)
	// Should report missing Href
	requireViolationContaining(t, result, "Href")
}

func TestLintLinkComplete(t *testing.T) {
	requireCleanLint(t, "link_complete.ui.go", `package test
import "github.com/DonaldMurillo/gofastr/core-ui/html"
func Ok() {
	html.Link(html.LinkConfig{Href: "/about", Text: "About"})
}
`)
}

func TestLintImageMissingSrc(t *testing.T) {
	dir := t.TempDir()
	content := `package test
import "github.com/DonaldMurillo/gofastr/core-ui/html"
func Bad() {
	html.Image(html.ImageConfig{Alt: "photo"})
}
`
	path := writeTempGoFile(t, dir, "image_no_src.ui.go", content)
	result, err := LintFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.HasErrors() {
		t.Fatal("expected violation for Image without Src")
	}
}

func TestLintImageComplete(t *testing.T) {
	requireCleanLint(t, "image_complete.ui.go", `package test
import "github.com/DonaldMurillo/gofastr/core-ui/html"
func Ok() {
	html.Image(html.ImageConfig{Src: "/img.jpg", Alt: "Photo"})
}
`)
}

func TestLintInputMissingFields(t *testing.T) {
	dir := t.TempDir()
	content := `package test
import "github.com/DonaldMurillo/gofastr/core-ui/html"
func Bad() {
	html.Input(html.InputConfig{Class: "field"})
}
`
	path := writeTempGoFile(t, dir, "input_missing.ui.go", content)
	result, err := LintFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.HasErrors() {
		t.Fatal("expected violations for Input without Type/Name")
	}
	// Should report both missing fields
	foundType, foundName := false, false
	for _, v := range result.Violations {
		if strings.Contains(v.Message, "Type") {
			foundType = true
		}
		if strings.Contains(v.Message, "Name") {
			foundName = true
		}
	}
	if !foundType {
		t.Error("expected missing Type violation")
	}
	if !foundName {
		t.Error("expected missing Name violation")
	}
}

func TestLintGroupMissingRole(t *testing.T) {
	dir := t.TempDir()
	content := `package test
import "github.com/DonaldMurillo/gofastr/core-ui/html"
func Bad() {
	html.Group(html.GroupConfig{AriaLabel: "info"})
}
`
	path := writeTempGoFile(t, dir, "group_no_role.ui.go", content)
	result, err := LintFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.HasErrors() {
		t.Fatal("expected violation for Group without Role")
	}
}

func TestLintGroupWithRole(t *testing.T) {
	requireCleanLint(t, "group_with_role.ui.go", `package test
import "github.com/DonaldMurillo/gofastr/core-ui/html"
func Ok() {
	html.Group(html.GroupConfig{Role: "status"})
}
`)
}

func TestLintDivNoRequiredFields(t *testing.T) {
	requireCleanLint(t, "div_empty.ui.go", `package test
import "github.com/DonaldMurillo/gofastr/core-ui/html"
func Ok() {
	html.Div(html.DivConfig{})
}
`)
}

func TestLintConfigPassedAsVariable(t *testing.T) {
	// When config is a variable (not a struct literal), linter can't check it
	requireCleanLint(t, "var_config.ui.go", `package test
import "github.com/DonaldMurillo/gofastr/core-ui/html"
func Ok(cfg html.NavConfig) {
	html.Nav(cfg)
}
`)
}
