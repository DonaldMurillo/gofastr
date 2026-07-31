package builtins

import (
	"context"
	"runtime"
	"strings"
	"testing"
)

func skipNonUnix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Bash tests require /bin/sh; skipping on Windows")
	}
}

func TestBashEcho(t *testing.T) {
	skipNonUnix(t)
	res, err := (Bash{}).Run(context.Background(), mustCall(t, map[string]any{
		"cmd": "echo hi",
	}), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("unexpected error: %+v", res)
	}
	if got := res.Content[0].Text; !strings.Contains(got, "hi") {
		t.Errorf("output = %q", got)
	}
}

func TestBashBlocklist(t *testing.T) {
	skipNonUnix(t)
	for _, banned := range DefaultBashBlocklist {
		res, _ := (Bash{}).Run(context.Background(), mustCall(t, map[string]any{
			"cmd": banned + " find-generic-password -s gofastr -w",
		}), nil)
		if !res.IsError {
			t.Errorf("Bash allowed banned command %q", banned)
		}
	}
}

func TestBashExitCodeSurfaced(t *testing.T) {
	skipNonUnix(t)
	res, _ := (Bash{}).Run(context.Background(), mustCall(t, map[string]any{
		"cmd": "exit 7",
	}), nil)
	if !res.IsError {
		t.Fatal("expected error on non-zero exit")
	}
	if !strings.Contains(res.Content[0].Text, "exit code 7") {
		t.Errorf("output missing exit code: %q", res.Content[0].Text)
	}
}

func TestBashTimeout(t *testing.T) {
	skipNonUnix(t)
	res, _ := (Bash{}).Run(context.Background(), mustCall(t, map[string]any{
		"cmd":        "sleep 5",
		"timeout_ms": 100,
	}), nil)
	if !res.IsError {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(res.Content[0].Text, "timeout") {
		t.Errorf("output missing timeout: %q", res.Content[0].Text)
	}
}

func TestLeadingCommand(t *testing.T) {
	cases := map[string]string{
		"ls -la":            "ls",
		"  cat file":        "cat",
		"echo;hi":           "echo;hi", // not split on `;` — tokenize on whitespace only
		";security delete":  "security",
		"|secret-tool find": "secret-tool",
		"(echo a)":          "echo",
	}
	for in, want := range cases {
		if got := leadingCommand(in); got != want {
			t.Errorf("leadingCommand(%q) = %q, want %q", in, got, want)
		}
	}
}
