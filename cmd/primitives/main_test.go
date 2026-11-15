package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

		{[]string{"bogus"}, 2, `unknown command "bogus"`},
	} {
		var stdout, stderr bytes.Buffer
		code := run("../..", tc.args, strings.NewReader(""), &stdout, &stderr)
		out := stdout.String() + stderr.String()
		if code != tc.code || !strings.Contains(out, tc.want) {
			t.Errorf("primitives %v: exit %d, want %d and output containing %q, got:\n%s", tc.args, code, tc.code, tc.want, out)
		}
	}
}

func TestGenCheckIsSilentWhenCurrent(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run("../..", []string{"gen", "--check"}, strings.NewReader(""), &stdout, &stderr); code != 0 || stdout.Len()+stderr.Len() != 0 {
		t.Errorf("gen --check on a current tree: exit %d, output %q %q", code, stdout.String(), stderr.String())
	}
}

func TestHook(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{
		"go.mod":                     "module example.test/m\n",
		"a/a.go":                     "package a\n",
		"b/b.go":                     "package b\n",
		"c/c.go":                     "package c\n",
		"primitives/x/primitive.yml": "primitive: x\nsummary: Does x.\ncode:\n  - a/\ntwins:\n  - job: Doing x\n    why: Not merged yet.\n    sites:\n      - path: a/a.go\n        note: first\n      - path: b/b.go\n        note: second\n",
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
	t.Cleanup(c.Close)
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
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(c.Close)
		var out, errOut bytes.Buffer
		if code := stop(c, findings, strings.NewReader(event), &out, &errOut); code != 0 {
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
	for _, want := range []string{"Does any other code path do the same job", "You changed x (Does x)", "docs/x.md", "Add an upgrade note"} {
		if code != 2 || !strings.Contains(msg, want) {
			t.Fatalf("code changed and its docs did not: stop = %d, message %q, want %q", code, msg, want)
		}
	}
	if code, msg := fire(session); code != 0 {
		t.Errorf("the same session was asked the same things twice: %d %s", code, msg)
	}
	write("docs/x.md", "# x\n\nOne is one.\n")
	code, msg = fire(`{"session_id":"s2"}`)
	if code != 2 || !strings.Contains(msg, "Add an upgrade note") || !strings.Contains(msg, "Does any other code path") {
		t.Errorf("the docs changed with the code: the on_change line and the question are still due, got %d %q", code, msg)
	}
	if strings.Contains(msg, "none of its docs changed") {
		t.Errorf("the docs changed, yet the notice asks for them: %q", msg)
	}
	if code, msg := fire(`{}`); code != 0 {
		t.Errorf("a session the hook cannot name is asked nothing, got %d %q", code, msg)
	}
	if passed := toldFiles(root, "stop-pass"); len(passed) != 1 {
		t.Errorf("a pass was not remembered: %v", passed)
	}
	groups := changedPackageGroups(root, []string{"a/a.go", "a/a_test.go", "a/testdata/x.go", "gone/g.go", "README.md"})
	if len(groups) != 1 || len(groups[0].pkgs) != 1 || groups[0].pkgs[0] != "./a" {
		t.Errorf("changedPackageGroups = %+v", groups)
	}
	write("testdata/go.mod", "module example.test/td\n")
	if got := changedModules(root, []string{"go.sum", "a/a.go", "gone/go.mod", "testdata/go.mod", "testdata/go.sum"}); len(got) != 1 || got[0] != root {
		t.Errorf("changedModules = %v, want the root module once (testdata skipped)", got)
	}
	write("gone.go", "")
	empty := treeState(root, "base", []string{"gone.go"})
	if err := os.Remove(filepath.Join(root, "gone.go")); err != nil {
		t.Fatal(err)
	}
	if deleted := treeState(root, "base", []string{"gone.go"}); deleted == empty {
		t.Error("a deleted file and an empty one hash alike: the second would skip its tests")
	}
}

// A nested go.mod is a different module. go vet from the repo root of
// a path under it fails with "main module does not contain package"
// (battery/print/chromepdf). The stop check must run that package inside
// the nested module, and still report a failure there.
func TestStopRunsNestedModuleInItsOwnModule(t *testing.T) {
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
	write("a/a.go", "package a\n")
	write("nested/go.mod", "module example.test/nested\n\ngo 1.24\n")
	write("nested/n.go", "package nested\n")
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
	fire := func() (int, string) {
		t.Helper()
		c, findings, err := primitives.Load(root)
		if err != nil || len(findings) > 0 {
			t.Fatal(err, findings)
		}
		t.Cleanup(c.Close)
		var out, errOut bytes.Buffer
		if code := stop(c, nil, strings.NewReader(`{}`), &out, &errOut); code != 0 {
			t.Fatalf("stop exits %d: %s", code, errOut.String())
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
	write("nested/n.go", "package nested\n\nfunc N() {}\n")
	code, msg := fire()
	if strings.Contains(msg, "does not contain package") {
		t.Fatalf("vet ran the nested package from the parent module: %s", msg)
	}
	if code != 0 {
		t.Fatalf("a passing nested-module change: stop = %d: %s", code, msg)
	}
	write("nested/n.go", "package nested\n\nvar _ = notDefined\n")
	code, msg = fire()
	if code != 2 || !strings.Contains(msg, "notDefined") {
		t.Fatalf("a compile error in the nested module: stop = %d, message %q", code, msg)
	}
	if strings.Contains(msg, "does not contain package") {
		t.Fatalf("the compile error was reported as a parent-module miss: %s", msg)
	}
}

// A nested testdata module is a fixture tree that is allowed not to
// compile (cmd/check-embed/testdata). Changing its go.mod must not send
// the session back for `go build ./...`.
func TestStopDoesNotBuildTestdataModule(t *testing.T) {
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
	write("a/a.go", "package a\n")
	write("testdata/go.mod", "module example.test/td\n\ngo 1.24\n")
	write("testdata/broken.go", "package td\n\nvar _ = doesNotExist\n")
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
	write("testdata/go.mod", "module example.test/td\n\ngo 1.24\n\n// changed\n")
	c, findings, err := primitives.Load(root)
	if err != nil || len(findings) > 0 {
		t.Fatal(err, findings)
	}
	t.Cleanup(c.Close)
	var out, errOut bytes.Buffer
	if code := stop(c, nil, strings.NewReader(`{}`), &out, &errOut); code != 0 {
		t.Fatalf("stop exits %d: %s", code, errOut.String())
	}
	if out.Len() != 0 {
		t.Fatalf("a testdata module that does not type-check blocked stop: %s", out.String())
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

func TestStopBlocksOnARegistryThatDoesNotParse(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{
		"go.mod":                     "module example.test/m\n\ngo 1.24\n",
		"a/a.go":                     "package a\n",
		"primitives/x/primitive.yml": "primitive: x\nsummary: Does x.\ncode:\n  - a/\n",
	} {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "-A"}, {"-c", "user.name=t", "-c", "user.email=t@example.invalid", "commit", "-q", "-m", "start"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	for name, tc := range map[string]struct{ body, want string }{
		"an unknown key": {"primitive: x\nsummary: Does x.\nowner: nobody\ncode:\n  - a/\n", "unknown key"},
		"malformed YAML": {"primitive: x\nsummary: Does x.\ncode:\n  - a/\n   bad: indent\n", "primitives/x/primitive.yml:"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(root, "primitives/x/primitive.yml"), []byte(tc.body), 0o600); err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			if code := run(root, []string{"stop"}, strings.NewReader(`{}`), &stdout, &stderr); code != 0 {
				t.Fatalf("stop exits %d on a broken registry; only a block decision sends the agent back", code)
			}
			if !strings.Contains(stdout.String(), `"decision":"block"`) || !strings.Contains(stdout.String(), tc.want) {
				t.Errorf("the stop was not blocked with the finding: %q", stdout.String())
			}
		})
	}
}

func TestStopKeepsWhatItFoundWhenACheckRunsOutOfTime(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{
		"go.mod":                     "module example.test/m\n\ngo 1.24\n",
		"a/a.go":                     "package a\n",
		"primitives/x/primitive.yml": "primitive: x\nsummary: Does x.\ncode:\n  - a/\n",
	} {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "-A"}, {"-c", "user.name=t", "-c", "user.email=t@example.invalid", "commit", "-q", "-m", "start"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	// A registry finding and a changed package, with no time for go vet.
	for name, body := range map[string]string{
		"a/a.go":                     "package a\n\nvar A = 1\n",
		"primitives/x/primitive.yml": "primitive: x\nsummary: Does x.\ncode:\n  - a/\ndocs:\n  - docs/gone.md\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	c, findings, err := primitives.Load(root)
	if err != nil || len(findings) > 0 {
		t.Fatal(err, findings)
	}
	t.Cleanup(c.Close)
	budget := stopBudget
	stopBudget = time.Nanosecond
	t.Cleanup(func() { stopBudget = budget })
	reason, warning := stopCheck(c, nil, strings.NewReader(`{"session_id":"s1"}`))
	if !strings.Contains(warning, "did not finish") {
		t.Fatalf("want the timeout warning, got %q", warning)
	}
	if !strings.Contains(reason, "docs/gone.md") {
		t.Errorf("the timeout dropped the registry finding: %q", reason)
	}
}

func TestStopBlocksOnARegistryThatCannotBeRead(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "primitives/x"), 0o700); err != nil {
		t.Fatal(err)
	}
	// A manifest that points outside the tree is one the loader refuses to read.
	outside := filepath.Join(t.TempDir(), "primitive.yml")
	if err := os.WriteFile(outside, []byte("primitive: x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "primitives/x/primitive.yml")); err != nil {
		t.Skip("no symlinks here:", err)
	}
	if _, _, err := primitives.Load(root); err == nil {
		t.Fatal("the fixture registry loaded")
	}
	var out, errOut bytes.Buffer
	if code := run(root, []string{"stop"}, strings.NewReader(`{}`), &out, &errOut); code != 0 {
		t.Fatalf("exit %d: only a block decision sends the agent back", code)
	}
	if !strings.Contains(out.String(), `"decision":"block"`) || !strings.Contains(out.String(), "cannot be read") {
		t.Errorf("no block decision: %q", out.String())
	}
	out.Reset()
	run(root, []string{"stop"}, strings.NewReader(`{"stop_hook_active":true}`), &out, &errOut)
	if out.Len() != 0 {
		t.Errorf("a session already sent back was blocked again: %q", out.String())
	}
}

func TestTheSameChangeOverAnotherBaseIsAnotherTree(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test/m\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	one := treeState(root, "1111111", []string{"go.mod"})
	if one == "" || one != treeState(root, "1111111", []string{"go.mod"}) {
		t.Fatalf("the state of an unchanged tree is not stable: %q", one)
	}
	if one == treeState(root, "2222222", []string{"go.mod"}) {
		t.Error("a pass over one base commit would be reused over another")
	}
}

func TestRelPathComparesPathSegments(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	outside := filepath.Join(filepath.Dir(root), "elsewhere", "a.go")
	for arg, want := range map[string]string{
		filepath.Join(root, "framework", "crud", "crud.go"): "framework/crud/crud.go",
		// A name that only starts with two dots is inside the root.
		filepath.Join(root, "..notes", "a.go"): "..notes/a.go",
		// Outside the root there is nothing to make it relative to.
		outside: filepath.ToSlash(outside),
	} {
		if got := relPath(root, arg); got != want {
			t.Errorf("relPath(%s) = %q, want %q", arg, got, want)
		}
	}
}

func TestAppliedRulesReachForAndStop(t *testing.T) {
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
	write("b/b.go", "package b\n\nfunc Two() int { return 2 }\n")
	write("primitives/x/primitive.yml", "primitive: x\nsummary: Does x.\ncode:\n  - a/\ninvariants:\n  - id: no-css\n    rule: No file ships its own CSS.\n    judge: Does this change add CSS outside the design system?\n    applies:\n      - b/\n")
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

	var stdout, stderr bytes.Buffer
	if code := run(root, []string{"for", "b/b.go"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("for b/b.go: exit %d: %s", code, stderr.String())
	}
	for _, want := range []string{"Also governed by rules held elsewhere", "x/no-css: No file ships its own CSS.", "judge: Does this change add CSS"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("for b/b.go output lacks %q:\n%s", want, stdout.String())
		}
	}
	if strings.Contains(stdout.String(), "no primitive owns") {
		t.Errorf("a file a rule applies to is not unowned:\n%s", stdout.String())
	}

	fire := func(event string) string {
		t.Helper()
		c, findings, err := primitives.Load(root)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(c.Close)
		var out, errOut bytes.Buffer
		stop(c, findings, strings.NewReader(event), &out, &errOut)
		var decision struct{ Decision, Reason string }
		if out.Len() > 0 {
			if err := json.Unmarshal(out.Bytes(), &decision); err != nil {
				t.Fatalf("stop printed %q: %v", out.String(), err)
			}
		}
		return decision.Reason
	}
	write("b/b.go", "package b\n\nfunc Two() int { return 1 + 1 }\n")
	msg := fire(`{"session_id":"s1"}`)
	for _, want := range []string{"You changed b/b.go, which x/no-css also governs", "Ask of your change: Does this change add CSS"} {
		if !strings.Contains(msg, want) {
			t.Errorf("stop after changing b/b.go lacks %q:\n%s", want, msg)
		}
	}
	if msg := fire(`{"session_id":"s1"}`); strings.Contains(msg, "x/no-css") {
		t.Errorf("the same session was shown the same rule twice:\n%s", msg)
	}
	git("add", "-A")
	git("commit", "-q", "-m", "go change")
	write("b/site.css", ".x { color: red; }\n")
	if msg := fire(`{"session_id":"s2"}`); !strings.Contains(msg, "x/no-css") {
		t.Errorf("a rule applied to a tree governs its CSS too, yet stop did not show it:\n%s", msg)
	}
}
