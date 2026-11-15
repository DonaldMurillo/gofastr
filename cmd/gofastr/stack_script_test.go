package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeStackGH stands in for gh: every call prints $BODY through its
// --jq expression with the real jq, and $CALLS records that gh ran.
const fakeStackGH = `#!/usr/bin/env bash
echo "$*" >> "$CALLS"
jqexpr=""
while [ $# -gt 0 ]; do
  case "$1" in --jq) jqexpr="$2"; shift 2 ;; *) shift ;; esac
done
if [ -n "$jqexpr" ]; then printf '%s' "$BODY" | jq -r "$jqexpr"; else printf '%s\n' "$BODY"; fi
`

func runStackScript(t *testing.T, body string, args ...string) (out string, code int, called bool) {
	t.Helper()
	for _, tool := range []string{"bash", "jq"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip(tool + " not on PATH")
		}
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(fakeStackGH), 0o700); err != nil {
		t.Fatal(err)
	}
	calls := filepath.Join(bin, "calls")
	script, err := filepath.Abs(filepath.Join("..", "..", "scripts", "stack.sh"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", append([]string{script}, args...)...)
	cmd.Env = append(os.Environ(),
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"BODY="+body, "CALLS="+calls)
	raw, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	_, statErr := os.Stat(calls)
	return string(raw), code, statErr == nil
}

// A call with the wrong arguments stops with the usage, before any
// request reaches GitHub.
//
//gofastr:test-reads scripts/stack.sh
func TestStackScriptRefusesBadArguments(t *testing.T) {
	for _, args := range [][]string{
		{}, {"bogus"}, {"create"}, {"create", "1"}, {"view"}, {"view", "1", "2"},
		{"merge"}, {"merge", "1", "merge", "extra"},
	} {
		out, code, called := runStackScript(t, "[]", args...)
		if code != 2 || called || !strings.Contains(out, "stack.sh create") {
			t.Errorf("stack.sh %v: exit %d, called gh %v, output:\n%s", args, code, called, out)
		}
	}
}

// A PR in no stack is reported as such, not as a jq error.
//
//gofastr:test-reads scripts/stack.sh
func TestStackScriptViewsAPRInNoStack(t *testing.T) {
	out, code, _ := runStackScript(t, "[]", "view", "12")
	if code != 0 || strings.TrimSpace(out) != "#12 is in no stack" {
		t.Fatalf("view of an unstacked PR: exit %d, output:\n%s", code, out)
	}
	stack := `[{"number":5,"base":{"ref":"main"},"open":true,"pull_requests":[{"number":1,"state":"open"},{"number":2,"state":"open"}]}]`
	out, code, _ = runStackScript(t, stack, "view", "2")
	if code != 0 || !strings.Contains(out, "stack #5 on main, open=true") || !strings.Contains(out, "  #2 open") {
		t.Fatalf("view of a stacked PR: exit %d, output:\n%s", code, out)
	}
}
