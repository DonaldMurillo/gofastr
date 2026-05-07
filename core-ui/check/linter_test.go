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

func TestLintValidFile(t *testing.T) {
	dir := t.TempDir()
	content := `package test
import "fmt"
import "github.com/gofastr/gofastr/core-ui/elements"
func Render() string { return fmt.Sprintf("hello") }
`
	path := writeTempGoFile(t, dir, "valid.ui.go", content)
	result, err := LintFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.HasErrors() {
		t.Errorf("expected no violations, got:\n%s", result.Error())
	}
}

func TestLintGoroutine(t *testing.T) {
	dir := t.TempDir()
	content := `package test
func Render() { go func() {}() }
`
	path := writeTempGoFile(t, dir, "goroutine.ui.go", content)
	result, err := LintFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.HasErrors() {
		t.Fatal("expected violations for goroutine, got none")
	}
	found := false
	for _, v := range result.Violations {
		if v.Message == "goroutines not allowed in .ui.go files" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected goroutine violation, got: %v", result.Violations)
	}
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
	dir := t.TempDir()
	content := `package test
import "os"
func Bad() { _ = os.Args }
`
	path := writeTempGoFile(t, dir, "os_import.ui.go", content)
	result, err := LintFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.HasErrors() {
		t.Fatal("expected violations for os import, got none")
	}
	found := false
	for _, v := range result.Violations {
		if v.Message == "os package not allowed in .ui.go files" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected os import violation, got: %v", result.Violations)
	}
}

func TestLintReflectImport(t *testing.T) {
	dir := t.TempDir()
	content := `package test
import "reflect"
func Bad() { reflect.TypeOf(42) }
`
	path := writeTempGoFile(t, dir, "reflect.ui.go", content)
	result, err := LintFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.HasErrors() {
		t.Fatal("expected violations for reflect import")
	}
	found := false
	for _, v := range result.Violations {
		if v.Message == "reflect package not allowed in .ui.go files" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected reflect import violation, got: %v", result.Violations)
	}
}

func TestLintAllowedImports(t *testing.T) {
	dir := t.TempDir()
	content := `package test
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
`
	path := writeTempGoFile(t, dir, "allowed.ui.go", content)
	result, err := LintFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.HasErrors() {
		t.Errorf("expected no violations for allowed imports, got:\n%s", result.Error())
	}
}

func TestLintFrameworkImports(t *testing.T) {
	dir := t.TempDir()
	content := `package test
import "github.com/gofastr/gofastr/core-ui/elements"
import "github.com/gofastr/gofastr/core-ui/component"
func Ok() { _ = elements.Div() }
`
	path := writeTempGoFile(t, dir, "framework.ui.go", content)
	result, err := LintFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.HasErrors() {
		t.Errorf("expected no violations for framework imports, got:\n%s", result.Error())
	}
}

func TestLintForLoop(t *testing.T) {
	dir := t.TempDir()
	content := `package test
func Ok() {
	for i := 0; i < 10; i++ { _ = i }
	for _, v := range []string{"a", "b"} { _ = v }
}
`
	path := writeTempGoFile(t, dir, "forloop.ui.go", content)
	result, err := LintFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.HasErrors() {
		t.Errorf("expected no violations for for loops, got:\n%s", result.Error())
	}
}

func TestLintIfElse(t *testing.T) {
	dir := t.TempDir()
	content := `package test
func Ok(x int) {
	if x > 0 {
		_ = x
	} else {
		_ = -x
	}
}
`
	path := writeTempGoFile(t, dir, "ifelse.ui.go", content)
	result, err := LintFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.HasErrors() {
		t.Errorf("expected no violations for if/else, got:\n%s", result.Error())
	}
}

func TestLintMakeChan(t *testing.T) {
	dir := t.TempDir()
	content := `package test
func Bad() { _ = make(chan int) }
`
	path := writeTempGoFile(t, dir, "makechan.ui.go", content)
	result, err := LintFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.HasErrors() {
		t.Fatal("expected violations for make(chan)")
	}
	found := false
	for _, v := range result.Violations {
		if strings.Contains(v.Message, "channel creation (make(chan))") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected channel creation violation, got: %v", result.Violations)
	}
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
	dir := t.TempDir()
	content := `package test
import "net/http"
func Bad() { http.Get("http://example.com") }
`
	path := writeTempGoFile(t, dir, "nethttp.ui.go", content)
	result, err := LintFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.HasErrors() {
		t.Fatal("expected violations for net/http import")
	}
	found := false
	for _, v := range result.Violations {
		if v.Message == "net/http package not allowed in .ui.go files" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected net/http violation, got: %v", result.Violations)
	}
}

func TestLintContextImport(t *testing.T) {
	dir := t.TempDir()
	content := `package test
import "context"
func Bad() { _ = context.Background() }
`
	path := writeTempGoFile(t, dir, "context.ui.go", content)
	result, err := LintFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.HasErrors() {
		t.Fatal("expected violations for context import")
	}
	found := false
	for _, v := range result.Violations {
		if v.Message == "context package not allowed in .ui.go files" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected context violation, got: %v", result.Violations)
	}
}
