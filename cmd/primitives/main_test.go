package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/primitives"
)

//gofastr:test-reads primitives/ *.go *.md
func TestCommands(t *testing.T) {
	for _, tc := range []struct {
		args []string
		code int
		want string
	}{
		{nil, 0, "crud: "},
		{[]string{"for", "framework/crud/nested_filter.go"}, 0, "crud.relations.nested-filter: "},
		{[]string{"for", "go.mod"}, 0, "no primitive owns this path yet"},
		{[]string{"show", "crud"}, 0, "child crud.scope: "},
		{[]string{"show", "crud.read-hooks"}, 0, "rejected Match by position: "},
		{[]string{"show", "nope"}, 2, "usage: primitives show"},
		{[]string{"lint"}, 0, "clean"},
		{[]string{"gen", "--check"}, 0, ""},
		{[]string{"bogus"}, 2, `unknown command "bogus"`},
	} {
		var stdout, stderr bytes.Buffer
		code := run("../..", tc.args, &stdout, &stderr)
		out := stdout.String() + stderr.String()
		if code != tc.code || !strings.Contains(out, tc.want) {
			t.Errorf("primitives %v: exit %d, want %d and output containing %q, got:\n%s", tc.args, code, tc.code, tc.want, out)
		}
	}
}

func TestHook(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{
		"go.mod":                     "module example.test/m\n",
		"a/a.go":                     "package a\n",
		"b/b.go":                     "package b\n",
		"c/c.go":                     "package c\n",
		"primitives/x/primitive.yml": "primitive: x\nsummary: Does x.\ncode:\n  - a/\ntwins:\n  - job: Doing x\n    sites:\n      - path: a/a.go\n        note: first\n      - path: b/b.go\n        note: second\n",
	} {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.invalid"}, args...)...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	git("add", "-A")
	git("commit", "-q", "-m", "start")
	c, findings, err := primitives.Load(root)
	if err != nil || len(findings) > 0 {
		t.Fatal(err, findings)
	}
	fire := func(event string) string {
		t.Helper()
		var out bytes.Buffer
		if code := hook(c, strings.NewReader(event), &out); code != 0 {
			t.Fatalf("exit %d", code)
		}
		return out.String()
	}
	const event = `{"hook_event_name":"PostToolUse","session_id":"s1","tool_name":"Bash"}`
	if got := fire(event); got != "" {
		t.Errorf("nothing changed, yet the hook said %q", got)
	}
	change := func(name string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte("package p // changed\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	change("c/c.go")
	if got := fire(event); got != "" {
		t.Errorf("a file in no twins entry changed, yet the hook said %q", got)
	}
	change("b/b.go")
	got := fire(event)
	for _, want := range []string{`"hookEventName":"PostToolUse"`, "You changed b/b.go", "a/a.go: first", "not finished until every site has it"} {
		if !strings.Contains(got, want) {
			t.Errorf("the notice lacks %q: %s", want, got)
		}
	}
	if got := fire(event); got != "" {
		t.Errorf("the same session was told twice: %q", got)
	}
	if got := fire(strings.Replace(event, "s1", "s2", 1)); !strings.Contains(got, "You changed b/b.go") {
		t.Errorf("a new session was not told: %q", got)
	}
	if got := fire(`not json`); got != "" {
		t.Errorf("unreadable input drew %q", got)
	}
}
