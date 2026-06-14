package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/codegen"
	"github.com/DonaldMurillo/gofastr/framework"
)

// ── theme.go ──────────────────────────────────────────────────────────

func TestRunThemeDispatch(t *testing.T) {
	out := covT_capStdout(t, func() { runTheme(nil) })
	if !strings.Contains(out, "Usage: gofastr theme") {
		t.Fatalf("help: %s", out)
	}
	out = covT_capStdout(t, func() { runTheme([]string{"help"}) })
	if !strings.Contains(out, "Subcommands") {
		t.Fatalf("help subcommand: %s", out)
	}
}

func TestRunThemeUnknownExits(t *testing.T) {
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { runTheme([]string{"frob"}) })
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestRunThemeInitWritesFile(t *testing.T) {
	dir := t.TempDir()
	covT_chdir(t, dir)
	covT_capStdout(t, func() { runThemeInit(nil) })
	if _, err := os.Stat(filepath.Join(dir, "theme", "theme.go")); err != nil {
		t.Fatalf("theme.go not written: %v", err)
	}
	// Second run without --force → exits.
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { runThemeInit(nil) })
	})
	if code != 1 {
		t.Fatalf("expected exit on existing, got %d", code)
	}
	// --force overwrites.
	covT_capStdout(t, func() { runThemeInit([]string{"--force"}) })
}

func TestRunThemeInitCustomOut(t *testing.T) {
	dir := t.TempDir()
	covT_chdir(t, dir)
	covT_capStdout(t, func() { runThemeInit([]string{"--out=custom/t.go"}) })
	if _, err := os.Stat(filepath.Join(dir, "custom", "t.go")); err != nil {
		t.Fatalf("custom out not written: %v", err)
	}
}

func TestRunThemeInitHelp(t *testing.T) {
	out := covT_capStdout(t, func() { runThemeInit([]string{"--help"}) })
	if !strings.Contains(out, "theme init") {
		t.Fatalf("help: %s", out)
	}
}

func TestRunThemeInitBadFlagExits(t *testing.T) {
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { runThemeInit([]string{"--nope"}) })
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

// ── docs.go ───────────────────────────────────────────────────────────

func TestPrintDocsHelp(t *testing.T) {
	out := covT_capStdout(t, func() { runDocs([]string{"--help"}) })
	if !strings.Contains(out, "browse framework docs") {
		t.Fatalf("docs help: %s", out)
	}
}

func TestRunDocsUnknownTopicExits(t *testing.T) {
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { runDocs([]string{"no-such-topic-xyz"}) })
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestRunDocsGrepEmptyTermExits(t *testing.T) {
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { runDocs([]string{"--grep"}) })
	})
	if code != 2 {
		t.Fatalf("want 2 got %d", code)
	}
}

func TestRunDocsGrepNoMatch(t *testing.T) {
	out := covT_capStdout(t, func() { runDocs([]string{"--grep", "zzzznevermatchqqq"}) })
	if !strings.Contains(out, "No matches") {
		t.Fatalf("expected no matches: %s", out)
	}
}

func TestPluralSAndHighlight(t *testing.T) {
	if pluralS(1) != "" || pluralS(2) != "es" {
		t.Fatal("pluralS")
	}
	if highlight("hello world", "") != "hello world" {
		t.Fatal("highlight empty term")
	}
	got := highlight("Foo foo FOO", "foo")
	if !strings.Contains(got, "Foo") {
		t.Fatalf("highlight: %s", got)
	}
}

// ── test_cmd.go ───────────────────────────────────────────────────────

func TestColorizeTestLineAllBranches(t *testing.T) {
	var p, f int
	covT_withTTY(func() {
		lines := []string{
			"=== RUN   TestX",
			"--- PASS: TestX (0.00s)",
			"--- FAIL: TestY (0.00s)",
			"ok  pkg 0.1s",
			"FAIL pkg",
			"PASS",
			"random line",
		}
		for _, l := range lines {
			_ = colorizeTestLine(l, &p, &f)
		}
	})
	if p != 1 || f != 1 {
		t.Fatalf("pass=%d fail=%d", p, f)
	}
}

// ── blueprint.go pure helpers ─────────────────────────────────────────

func TestBlueprintTitleOrName(t *testing.T) {
	if (BlueprintScreen{Title: "T", Name: "N"}).TitleOrName() != "T" {
		t.Fatal("title preferred")
	}
	if (BlueprintScreen{Name: "N"}).TitleOrName() != "N" {
		t.Fatal("name fallback")
	}
}

func TestRenderBlueprintBlockTypes(t *testing.T) {
	cases := []struct {
		block BlueprintBlock
		want  string
	}{
		{BlueprintBlock{Type: "p", Text: "hi"}, "render.Tag(\"p\""},
		{BlueprintBlock{Type: "heading", Level: 2, Text: "H"}, "html.Heading"},
		{BlueprintBlock{Type: "h3", Text: "H"}, "Level: 3"},
		{BlueprintBlock{Type: "h6", Text: "H"}, "Level: 6"},
		{BlueprintBlock{Type: "link", Href: "/x", Text: "L"}, "html.Link"},
		{BlueprintBlock{Type: "section", Text: "S", Class: "c"}, "section"},
		{BlueprintBlock{Type: "weird", Text: "D"}, "div"},
	}
	for _, c := range cases {
		got := renderBlueprintBlockForScreen(BlueprintScreen{}, c.block, nil, nil, "/api")
		if !strings.Contains(got, c.want) {
			t.Errorf("block %q → %s, want substring %q", c.block.Type, got, c.want)
		}
	}
}

func TestBlueprintEntityTableAndDefault(t *testing.T) {
	if blueprintEntityTable(framework.EntityDeclaration{Table: "/users/"}) != "users" {
		t.Fatal("explicit table trimmed")
	}
	if blueprintEntityTable(framework.EntityDeclaration{Name: "UserProfile"}) != "user_profile" {
		t.Fatalf("default = %q", blueprintEntityTable(framework.EntityDeclaration{Name: "UserProfile"}))
	}
	if blueprintDefaultTableName("already_snake") != "already_snake" {
		t.Fatal("lowercase passthrough")
	}
	// Mixed-case with separators: separators normalize to "_" and the
	// camel-split inserts another "_" before each uppercase letter.
	if got := blueprintDefaultTableName("With-Dash And Space"); got != "with__dash__and__space" {
		t.Fatalf("got %q", got)
	}
	// All-lowercase camel path: PascalCase → snake.
	if got := blueprintDefaultTableName("OrderItem"); got != "order_item" {
		t.Fatalf("got %q", got)
	}
}

func TestBlueprintActionNameAndEvent(t *testing.T) {
	if blueprintActionName(BlueprintAction{Name: "Save"}) != "Save" {
		t.Fatal("explicit name")
	}
	if blueprintActionName(BlueprintAction{Event: "Submit"}) != "submit" {
		t.Fatal("event fallback")
	}
	if blueprintActionEvent(BlueprintAction{}) != "click" {
		t.Fatal("default click")
	}
	// Empty/only-separator name falls back to the "screen" sentinel id.
	if got := screenActionComponentID(BlueprintScreen{Name: "/"}); got != "screen-screen" {
		t.Fatalf("sentinel id = %q", got)
	}
	if got := screenActionComponentID(BlueprintScreen{Name: "My Page"}); !strings.HasPrefix(got, "screen-") {
		t.Fatalf("id = %q", got)
	}
}

// ── audit_lint.go formatLintReport ────────────────────────────────────

func TestFormatLintReport(t *testing.T) {
	if !strings.Contains(formatLintReport(nil), "No findings") {
		t.Fatal("empty report")
	}
	out := formatLintReport([]LintFinding{
		{File: "a.go", Line: 3, Rule: "R", Message: "boom", Snippet: "x := 1"},
		{File: "b.go", Line: 9, Rule: "R2", Message: "bad"},
	})
	for _, want := range []string{"2 issue", "a.go:3", "[R]", "boom", "x := 1", "b.go:9"} {
		if !strings.Contains(out, want) {
			t.Errorf("report missing %q:\n%s", want, out)
		}
	}
}

// ── generate.go printCodegenFilesJSON ─────────────────────────────────

func TestPrintCodegenFilesJSON(t *testing.T) {
	out := covT_capStdout(t, func() {
		printCodegenFilesJSON([]codegen.GeneratedFile{
			{Path: "x.go", Content: "abc"},
			{Path: "y.go", Content: "de"},
		})
	})
	if !strings.Contains(out, `"path":"x.go"`) || !strings.Contains(out, `"size":3`) {
		t.Fatalf("json: %s", out)
	}
}
