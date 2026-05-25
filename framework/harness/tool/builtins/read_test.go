package builtins

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/harness/tool"
)

func mustCall(t *testing.T, args any) tool.ToolCall {
	t.Helper()
	b, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	return tool.ToolCall{ID: ids.NewCallID(), Input: b}
}

func TestRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(path, []byte("hello\nworld\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := Read{}
	res, err := r.Run(context.Background(), mustCall(t, map[string]any{"path": path}), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("unexpected error: %+v", res)
	}
	if got := res.Content[0].Text; !strings.Contains(got, "hello") || !strings.Contains(got, "world") {
		t.Fatalf("missing content: %q", got)
	}
}

func TestReadLineLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(path, []byte("a\nb\nc\nd\ne\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := Read{}
	res, _ := r.Run(context.Background(), mustCall(t, map[string]any{"path": path, "limit": 2}), nil)
	got := res.Content[0].Text
	if got != "a\nb\n" {
		t.Fatalf("limit=2 returned %q", got)
	}
}

func TestReadMissingFile(t *testing.T) {
	res, _ := Read{}.Run(context.Background(), mustCall(t, map[string]any{"path": "/no/such/file"}), nil)
	if !res.IsError {
		t.Fatal("expected IsError for missing file")
	}
}

func TestLs(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "a.txt"), nil, 0o600)
	_ = os.WriteFile(filepath.Join(dir, "b.txt"), nil, 0o600)
	_ = os.Mkdir(filepath.Join(dir, "sub"), 0o700)

	res, _ := Ls{}.Run(context.Background(), mustCall(t, map[string]any{"path": dir}), nil)
	got := res.Content[0].Text
	for _, want := range []string{"a.txt", "b.txt", "sub/"} {
		if !strings.Contains(got, want) {
			t.Errorf("Ls missing %q in: %q", want, got)
		}
	}
}

func TestGlob(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "x.go"), nil, 0o600)
	_ = os.MkdirAll(filepath.Join(dir, "a", "b"), 0o700)
	_ = os.WriteFile(filepath.Join(dir, "a", "b", "y.go"), nil, 0o600)
	_ = os.WriteFile(filepath.Join(dir, "z.txt"), nil, 0o600)

	res, _ := Glob{}.Run(context.Background(), mustCall(t, map[string]any{
		"pattern": "**/*.go",
		"root":    dir,
	}), nil)
	got := res.Content[0].Text
	if !strings.Contains(got, "x.go") || !strings.Contains(got, "y.go") {
		t.Fatalf("Glob missed matches: %q", got)
	}
	if strings.Contains(got, "z.txt") {
		t.Fatalf("Glob included non-matching: %q", got)
	}
}

func TestGrep(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "a.txt"), []byte("foo\nbar\nfoo bar\n"), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "b.txt"), []byte("nothing matches here\n"), 0o600)

	res, _ := Grep{}.Run(context.Background(), mustCall(t, map[string]any{
		"pattern": "^foo",
		"path":    dir,
	}), nil)
	got := res.Content[0].Text
	if !strings.Contains(got, "a.txt:1: foo") || !strings.Contains(got, "a.txt:3: foo bar") {
		t.Fatalf("Grep matches missing: %q", got)
	}
	if strings.Contains(got, "b.txt") {
		t.Fatalf("Grep included file with no matches: %q", got)
	}
}

func TestSourceTools(t *testing.T) {
	tools, err := Source{}.Tools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// v0.1: Read, Write, Edit, Bash, Grep, Glob, Ls, WebFetch,
	// TaskList, Agent, ToolSearch = 11 tools. Adjust when adding more.
	if len(tools) != 11 {
		t.Fatalf("Source default exposes %d tools, want 11", len(tools))
	}
}

func TestSourceEnabledPacks(t *testing.T) {
	tools, _ := Source{EnabledPacks: []string{"fs"}}.Tools(context.Background())
	got := map[string]bool{}
	for _, t := range tools {
		got[t.Name()] = true
	}
	for _, want := range []string{"Read", "Write", "Edit", "Glob", "Ls"} {
		if !got[want] {
			t.Errorf("fs pack missing %q", want)
		}
	}
	if got["Bash"] {
		t.Error("fs pack should not include Bash")
	}
}
