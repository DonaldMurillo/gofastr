package builtins

import (
	"context"
	"strings"
	"testing"
)

// assertBashBlocked runs cmd through the Bash tool and asserts the
// credential-exfil blocklist refuses it with a "blocked" diagnostic;
// label carries the leg's own wording into the failure message.
func assertBashBlocked(t *testing.T, label, cmd string) {
	res, _ := (Bash{}).Run(context.Background(), mustCall(t, map[string]any{
		"cmd": cmd,
	}), nil)
	if res == nil || !res.IsError || !strings.Contains(res.Content[0].Text, "blocked") {
		t.Errorf("Bash allowed %s %q: %+v", label, cmd, res)
	}
}

// assertBashClean runs cmd through the Bash tool and asserts it executes
// clean, proving the blocklist refuses by content, not blunt rejection.
func assertBashClean(t *testing.T, cmd string) {
	res, _ := (Bash{}).Run(context.Background(), mustCall(t, map[string]any{
		"cmd": cmd,
	}), nil)
	if res.IsError {
		t.Errorf("Bash blocked benign command: %+v", res)
	}
}

// TestBashBlocklistBypass asserts the credential-exfil blocklist
// blocks a banned command regardless of invocation form: absolute
// path, benign-token-then-separator, or `command`/`env` prefix.
func TestBashBlocklistBypass(t *testing.T) {
	skipNonUnix(t)
	blocked := []string{
		"/usr/bin/security dump-keychain",      // absolute path
		"true; security find-generic-password", // separator after benign token
		"command secret-tool lookup x y",       // `command` builtin prefix
		"echo a && keyctl list @u",             // && chained after benign token
	}
	for _, cmd := range blocked {
		assertBashBlocked(t, "blocklist-bypass command", cmd)
	}
	// Happy path: a benign command that merely shares a prefix is allowed.
	assertBashClean(t, "echo securely")
}

// TestBashBlocklistShellForms asserts the blocklist also catches a
// banned command hidden behind shell quoting / substitution forms:
// backtick, $(), quoted name, and a leading backslash. This is
// best-effort defense-in-depth behind the permission middleware.
func TestBashBlocklistShellForms(t *testing.T) {
	skipNonUnix(t)
	blocked := []string{
		"echo `security find-generic-password`", // backtick substitution
		"echo $(security dump-keychain)",        // $() substitution
		`"security" dump-keychain`,              // double-quoted name
		`\security dump-keychain`,               // leading backslash
	}
	for _, cmd := range blocked {
		assertBashBlocked(t, "shell-form blocklist bypass", cmd)
	}
	// Happy path: a benign substitution is allowed.
	assertBashClean(t, "echo $(echo hi)")
}
