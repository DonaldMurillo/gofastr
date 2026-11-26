package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func fixtureRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
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
	return root
}

func TestOnlyLintAndStopRunOnARegistryThatDoesNotParse(t *testing.T) {
	root := fixtureRepo(t, map[string]string{
		"go.mod":                     "module example.test/m\n",
		"a/a.go":                     "package a\n",
		"primitives/x/primitive.yml": "primitive: x\nsummary: Does x.\nowner: nobody\ncode:\n  - a/\n",
	})
	for _, args := range [][]string{nil, {"for", "a/a.go"}, {"show", "x"}, {"gen", "--check"}, {"judge"}} {
		var stdout, stderr bytes.Buffer
		if code := run(root, args, strings.NewReader(""), &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "does not parse") {
			t.Errorf("primitives %v on a broken registry: exit %d, stderr %q; want 1 and the findings", args, code, stderr.String())
		}
	}
	var stdout, stderr bytes.Buffer
	if code := run(root, []string{"lint"}, strings.NewReader(""), &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "unknown key") {
		t.Errorf("lint on a broken registry: exit %d, stderr %q; want 1 and the finding", code, stderr.String())
	}
}

func TestHookReadsTheEventRunIsGiven(t *testing.T) {
	root := fixtureRepo(t, map[string]string{
		"go.mod":                     "module example.test/m\n",
		"a/a.go":                     "package a\n",
		"b/b.go":                     "package b\n",
		"primitives/x/primitive.yml": "primitive: x\nsummary: Does x.\ncode:\n  - a/\ntwins:\n  - job: Doing x\n    why: Not merged yet.\n    sites:\n      - path: a/a.go\n        note: first\n      - path: b/b.go\n        note: second\n",
	})
	if err := os.WriteFile(filepath.Join(root, "b/b.go"), []byte("package b // changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	event := `{"hook_event_name":"PostToolUse","session_id":"s1"}`
	if code := run(root, []string{"hook"}, strings.NewReader(event), &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "You changed b/b.go") {
		t.Errorf("primitives hook: exit %d, stdout %q; want the same-job notice for the event on its stdin", code, stdout.String())
	}
}

func TestStopSkipsTestdataPackages(t *testing.T) {
	root := fixtureRepo(t, map[string]string{
		"go.mod":            "module example.test/m\n",
		"a/a.go":            "package a\n",
		"a/testdata/bad.go": "package bad\n",
	})
	groups := changedPackageGroups(root, []string{"a/a.go", "a/testdata/bad.go"})
	if len(groups) != 1 || len(groups[0].pkgs) != 1 || groups[0].pkgs[0] != "./a" {
		t.Errorf("changedPackageGroups = %+v, want ./a alone: a testdata tree exists to be wrong", groups)
	}
}

func TestStopDoesNotRetestATreeThatPassed(t *testing.T) {
	root := fixtureRepo(t, map[string]string{
		".gitignore":                 ".gofastr/\n",
		"go.mod":                     "module example.test/m\n\ngo 1.24\n",
		"a/a.go":                     "package a\n\nfunc One() int { return 1 }\n",
		"a/a_test.go":                "package a\n\nimport \"testing\"\n\nfunc TestOne(t *testing.T) {\n\tif One() != 1 {\n\t\tt.Fatal(\"One is not 1\")\n\t}\n}\n",
		"primitives/x/primitive.yml": "primitive: x\nsummary: Does x.\ncode:\n  - a/\n",
	})
	if err := os.WriteFile(filepath.Join(root, "a/a.go"), []byte("package a\n\nfunc One() int { return 2 }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var control bytes.Buffer
	if run(root, []string{"stop"}, strings.NewReader(`{}`), &control, io.Discard); !strings.Contains(control.String(), "One is not 1") {
		t.Fatalf("control: stop over the failing tree did not report the failure: %q", control.String())
	}
	head, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	rememberTold(root, "stop-pass", []string{treeState(root, strings.TrimSpace(string(head)), []string{"a/a.go"})})
	var stdout, stderr bytes.Buffer
	if code := run(root, []string{"stop"}, strings.NewReader(`{}`), &stdout, &stderr); code != 0 || stdout.Len() != 0 || stderr.Len() != 0 {
		t.Errorf("stop over a tree recorded as passing: exit %d, stdout %q, stderr %q; want it let go without running the tests again", code, stdout.String(), stderr.String())
	}
}

func TestHookStateIsKeptOnlyForANameableSession(t *testing.T) {
	root := t.TempDir()
	for _, session := range []string{"", "a b", "../escape", "a/b", strings.Repeat("x", 81)} {
		rememberTold(root, session, []string{"f.go"})
		if got := toldFiles(root, session); got != nil {
			t.Errorf("session %q was remembered as %v: a name outside the pattern must be told every time", session, got)
		}
	}
	if entries, _ := os.ReadDir(filepath.Join(root, hookStateDir)); len(entries) > 0 {
		t.Errorf("an unnameable session wrote hook state: %v", entries)
	}
	if err := os.MkdirAll(filepath.Join(root, hookStateDir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, hookStateDir, "a b"), []byte("f.go\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := toldFiles(root, "a b"); got != nil {
		t.Errorf("state planted under an unnameable session was read: %v", got)
	}
	rememberTold(root, "ok-1_A", []string{"f.go"})
	if got := toldFiles(root, "ok-1_A"); len(got) != 1 || got[0] != "f.go" {
		t.Errorf("a nameable session was not remembered: %v", got)
	}
	info, err := os.Stat(filepath.Join(root, hookStateDir, "ok-1_A"))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("hook state file mode %v, want 0600", info.Mode().Perm())
	}
}
