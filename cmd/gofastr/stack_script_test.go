package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fakeStackGH stands in for gh: every call prints $BODY through its
// --jq expression with the real jq, $CALLS records that gh ran, and a
// request body read from stdin (--input -) is saved to $STDIN. A
// poll of a running merge (merge-async/<id>) prints $POLL instead, and a
// call that starts a merge exits $PUT_EXIT after printing, as gh does on
// an HTTP error.
const fakeStackGH = `#!/usr/bin/env bash
args="$*"
echo "$args" >> "$CALLS"
case "$args" in *"--input -"*) cat > "$STDIN" ;; esac
case "$args" in *merge-async/*) BODY="$POLL" ;; esac
jqexpr=""
while [ $# -gt 0 ]; do
  case "$1" in --jq) jqexpr="$2"; shift 2 ;; *) shift ;; esac
done
if [ -n "$jqexpr" ]; then printf '%s' "$BODY" | jq -r "$jqexpr"; else printf '%s\n' "$BODY"; fi
case "$args" in *"-X PUT"*merge-async*) exit "${PUT_EXIT:-0}" ;; esac
`

func runStackScript(t *testing.T, body string, args ...string) (out string, code int, called bool) {
	t.Helper()
	return runStackScriptEnv(t, body, nil, args...)
}

// stackCalls runs stack.sh and returns its output and the gh command lines it ran.
func stackCalls(t *testing.T, body string, args ...string) (out string, code int, calls string) {
	t.Helper()
	out, code, _ = runStackScript(t, body, args...)
	return out, code, lastCalls
}

var lastCalls, lastStdin string

func runStackScriptEnv(t *testing.T, body string, env []string, args ...string) (out string, code int, called bool) {
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
	stdin := filepath.Join(bin, "stdin")
	script, err := filepath.Abs(filepath.Join("..", "..", "scripts", "stack.sh"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", append([]string{script}, args...)...)
	cmd.Env = append(os.Environ(),
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"BODY="+body, "CALLS="+calls, "STDIN="+stdin)
	cmd.Env = append(cmd.Env, env...)
	raw, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	logged, statErr := os.ReadFile(calls)
	lastCalls = string(logged)
	sent, _ := os.ReadFile(stdin)
	lastStdin = string(sent)
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
		{"add"}, {"add", "5"}, {"unstack"}, {"unstack", "5", "6"},
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

// A merge already running answers 409 with its id: the script polls that
// merge instead of failing. A merge already done ends at once, and a
// refusal with no id fails.
//
//gofastr:test-reads scripts/stack.sh
func TestStackScriptMergeResumesARunningMerge(t *testing.T) {
	running := `{"message":"A merge request already exists","status":"pending","details":{"uuid":"u-1"}}`
	out, code, _ := runStackScriptEnv(t, running, []string{"PUT_EXIT=1", `POLL={"status":"merged"}`}, "merge", "9")
	if code != 0 || !strings.Contains(out, "merge u-1: merged") {
		t.Fatalf("409 for a running merge: exit %d, output:\n%s", code, out)
	}
	failed := `{"status":"failed","details":{"message":"Required status check \"ci\" is failing"}}`
	out, code, _ = runStackScriptEnv(t, running, []string{"PUT_EXIT=1", "POLL=" + failed}, "merge", "9")
	if code == 0 || !strings.Contains(out, "merge u-1: failed") || !strings.Contains(out, `Required status check "ci" is failing`) {
		t.Fatalf("a merge GitHub rejects must name the reason: exit %d, output:\n%s", code, out)
	}
	out, code, _ = runStackScriptEnv(t, `{"status":"merged"}`, nil, "merge", "9")
	if code != 0 || strings.TrimSpace(out) != "merge: merged" {
		t.Fatalf("merge already done: exit %d, output:\n%s", code, out)
	}
	out, code, _ = runStackScriptEnv(t, `{"message":"Not mergeable"}`, []string{"PUT_EXIT=1"}, "merge", "9")
	if code == 0 || !strings.Contains(out, "Not mergeable") {
		t.Fatalf("a refusal with no id: exit %d, output:\n%s", code, out)
	}
}

// add posts the new PRs to the stack's add route and prints the stack
// GitHub returns; a series opened later joins its stack this way, since
// create refuses a PR already in one.
//
//gofastr:test-reads scripts/stack.sh
func TestStackScriptAddsPRsToAStack(t *testing.T) {
	stack := `{"number":5,"base":{"ref":"main"},"pull_requests":[{"number":1},{"number":2},{"number":3},{"number":4}]}`
	out, code, calls := stackCalls(t, stack, "add", "5", "3", "4")
	if code != 0 || strings.TrimSpace(out) != "stack #5 on main: #1 -> #2 -> #3 -> #4" {
		t.Fatalf("add: exit %d, output:\n%s", code, out)
	}
	if !strings.Contains(calls, "-X POST repos/DonaldMurillo/gofastr/stacks/5/add") {
		t.Fatalf("add called gh as:\n%s", calls)
	}
	var sent struct {
		PullRequests []int `json:"pull_requests"`
	}
	if err := json.Unmarshal([]byte(lastStdin), &sent); err != nil || !slices.Equal(sent.PullRequests, []int{3, 4}) {
		t.Fatalf("add sent %q (%v), want pull_requests [3 4] in that order", lastStdin, err)
	}
}

// unstack reports a stack GitHub dissolved (204, no body) and one it kept
// because some PRs could not leave it.
//
//gofastr:test-reads scripts/stack.sh
func TestStackScriptUnstacks(t *testing.T) {
	out, code, calls := stackCalls(t, "", "unstack", "5")
	if code != 0 || strings.TrimSpace(out) != "stack #5 dissolved" || !strings.Contains(calls, "-X POST repos/DonaldMurillo/gofastr/stacks/5/unstack") {
		t.Fatalf("unstack of a whole stack: exit %d, output:\n%s\ncalls:\n%s", code, out, calls)
	}
	out, code, _ = stackCalls(t, `{"number":5,"pull_requests":[{"number":1}]}`, "unstack", "5")
	if code != 0 || strings.TrimSpace(out) != "stack #5 kept: #1" {
		t.Fatalf("unstack that keeps a merged PR: exit %d, output:\n%s", code, out)
	}
}
