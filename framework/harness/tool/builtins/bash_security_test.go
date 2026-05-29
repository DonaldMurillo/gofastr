package builtins

import (
	"context"
	"strings"
	"testing"
)

// TestBashBlocklistBypass asserts the credential-exfil blocklist
// blocks a banned command regardless of invocation form: absolute
// path, benign-token-then-separator, or `command`/`env` prefix.
func TestBashBlocklistBypass(t *testing.T) {
	skipNonUnix(t)
	blocked := []string{
		"/usr/bin/security dump-keychain",   // absolute path
		"true; security find-generic-password", // separator after benign token
		"command secret-tool lookup x y",    // `command` builtin prefix
		"echo a && keyctl list @u",          // && chained after benign token
	}
	for _, cmd := range blocked {
		res, _ := (Bash{}).Run(context.Background(), mustCall(t, map[string]any{
			"cmd": cmd,
		}), nil)
		if res == nil || !res.IsError || !strings.Contains(res.Content[0].Text, "blocked") {
			t.Errorf("Bash allowed blocklist-bypass command %q: %+v", cmd, res)
		}
	}
	// Happy path: a benign command that merely shares a prefix is allowed.
	res, _ := (Bash{}).Run(context.Background(), mustCall(t, map[string]any{
		"cmd": "echo securely",
	}), nil)
	if res.IsError {
		t.Errorf("Bash blocked benign command: %+v", res)
	}
}
