package main

import (
	"bytes"
	"encoding/json"
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

func TestStop(t *testing.T) {
	root := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.test/m\n\ngo 1.24\n")
	write("a/a.go", "package a\n\nfunc One() int { return 1 }\n")
	write("a/a_test.go", "package a\n\nimport \"testing\"\n\nfunc TestOne(t *testing.T) {\n\tif One() != 1 {\n\t\tt.Fatal(\"One is not 1\")\n\t}\n}\n")
	write("primitives/x/primitive.yml", "primitive: x\nsummary: Does x.\ncode:\n  - a/\n")
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
	fire := func(event string) (int, string) {
		t.Helper()
		c, findings, err := primitives.Load(root)
		if err != nil || len(findings) > 0 {
			t.Fatal(err, findings)
		}
		var out, errOut bytes.Buffer
		if code := stop(c, strings.NewReader(event), &out, &errOut); code != 0 {
			t.Fatalf("stop exits %d: a hook run through go run can only speak through its output", code)
		}
		if out.Len() == 0 {
			return 0, errOut.String()
		}
		var decision struct{ Decision, Reason string }
		if err := json.Unmarshal(out.Bytes(), &decision); err != nil || decision.Decision != "block" {
			t.Fatalf("stop printed %q, want a block decision (%v)", out.String(), err)
		}
		return 2, decision.Reason
	}
	if code, msg := fire(`{}`); code != 0 {
		t.Fatalf("nothing changed, yet stop = %d: %s", code, msg)
	}
	write("a/a.go", "package a\n\nfunc One() int { return 2 }\n")
	code, msg := fire(`{}`)
	if code != 2 || !strings.Contains(msg, "One is not 1") {
		t.Fatalf("a failing test: stop = %d, message %q", code, msg)
	}
	if code, _ := fire(`{"stop_hook_active":true}`); code != 0 {
		t.Errorf("a session already sent back once must be let go, got %d", code)
	}
	write("a/a.go", "package a\n\n// One is one.\nfunc One() int { return 1 }\n")
	if code, msg := fire(`{}`); code != 0 {
		t.Fatalf("a passing change: stop = %d: %s", code, msg)
	}
	git("add", "-A")
	git("commit", "-q", "-m", "pass")
	write("a/a.go", "package a\n\n// One returns one, always.\nfunc One() int { return 1 }\n")
	if !commentOnly(root, "HEAD", "a/a.go") {
		t.Error("an edit to a comment alone is comment-only")
	}
	if code, msg := fire(`{"session_id":"c"}`); code != 0 {
		t.Errorf("a comment-only edit: stop = %d: %s", code, msg)
	}
	write("a/a.go", "//go:build linux\n\npackage a\n\n// One is one.\nfunc One() int { return 1 }\n")
	if commentOnly(root, "HEAD", "a/a.go") {
		t.Error("a new build constraint is a comment that changes the code")
	}
	write("a/a.go", "package a\n\n// One is one.\nfunc One() int { return 1 + 0 }\n")
	if commentOnly(root, "HEAD", "a/a.go") {
		t.Error("a changed expression is not comment-only")
	}
	if commentOnly(root, "HEAD", "a/new.go") {
		t.Error("a file with no base version is not comment-only")
	}
	write("docs/x.md", "# x\n")
	write("primitives/x/primitive.yml", "primitive: x\nsummary: Does x.\ncode:\n  - a/\ndocs:\n  - docs/x.md\non_change:\n  - Add an upgrade note when a request is newly refused.\n")
	git("add", "docs", "primitives")
	git("commit", "-q", "-m", "docs")
	const session = `{"session_id":"s1"}`
	code, msg = fire(session)
	for _, want := range []string{"You changed x (Does x)", "docs/x.md", "Add an upgrade note"} {
		if code != 2 || !strings.Contains(msg, want) {
			t.Fatalf("code changed and its docs did not: stop = %d, message %q, want %q", code, msg, want)
		}
	}
	if code, msg := fire(session); code != 0 {
		t.Errorf("the same session was asked about the same docs twice: %d %s", code, msg)
	}
	write("docs/x.md", "# x\n\nOne is one.\n")
	if code, msg := fire(`{"session_id":"s2"}`); code != 0 {
		t.Errorf("the docs changed with the code, yet stop = %d: %s", code, msg)
	}
	if passed := toldFiles(root, "stop-pass"); len(passed) != 1 {
		t.Errorf("a pass was not remembered: %v", passed)
	}
	if got := changedPackages(root, []string{"a/a.go", "a/a_test.go", "a/testdata/x.go", "gone/g.go", "README.md"}); len(got) != 1 || got[0] != "./a" {
		t.Errorf("changedPackages = %v", got)
	}
}

func TestFailuresKeepsWhatFailed(t *testing.T) {
	out := strings.Join([]string{
		"  server ready", "  Listening: http://127.0.0.1:1", "=== RUN   TestA",
		"--- FAIL: TestA (0.01s)", "    a_test.go:12: got 2, want 1",
		"  more noise", "FAIL", "FAIL\texample.test/m/a\t0.1s", "ok  \texample.test/m/b\t0.1s",
	}, "\n")
	got := failures(out, 40)
	for _, want := range []string{"--- FAIL: TestA", "a_test.go:12: got 2, want 1", "FAIL\texample.test/m/a"} {
		if !strings.Contains(got, want) {
			t.Errorf("failures dropped %q:\n%s", want, got)
		}
	}
	for _, noise := range []string{"server ready", "Listening", "more noise", "ok  "} {
		if strings.Contains(got, noise) {
			t.Errorf("failures kept %q:\n%s", noise, got)
		}
	}
	if got := failures("./a.go:3:5: undefined: x", 40); !strings.Contains(got, "undefined: x") {
		t.Errorf("a vet finding was dropped: %q", got)
	}
	if got := failures("line one\nline two", 1); got != "line two" {
		t.Errorf("with nothing recognisable, want the tail, got %q", got)
	}
}
