package builtins

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/harness/tool"
)

// bashImpl runs a shell command via /bin/sh -c. Defaults:
//
//   - 2-minute timeout (configurable per-call up to 10 minutes)
//   - 1 MiB combined stdout+stderr cap
//   - Default-blocked credential-exfil commands (security, secret-tool,
//     keyctl, kwalletcli) — defense in depth alongside the permission
//     middleware. The blocklist runs before any sandbox.
//
// Sandbox integration: pluggable via SandboxFn. When non-nil, it
// wraps the *exec.Cmd before Run. Defaults to identity on platforms
// without sandbox support.
type bashImpl struct {
	// SandboxFn wraps the command for OS-level sandboxing. Engine
	// wires `sandbox-exec` on macOS / `bwrap` on Linux when
	// available; nil = no sandbox.
	SandboxFn func(*exec.Cmd) *exec.Cmd

	// ExtraBlocklist adds command names to the default blocklist.
	ExtraBlocklist []string
}

// DefaultBashBlocklist names the commands the Bash tool refuses to
// run by default. The threat model rationale is in
// docs/harness-architecture.md § Threat model → Standing rules.
var DefaultBashBlocklist = []string{
	"security",     // macOS keychain CLI
	"secret-tool",  // Linux libsecret CLI
	"keyctl",       // Linux kernel keyring
	"kwalletcli",   // KDE wallet CLI
	"systemd-ask-password",
}

func (bashImpl) Name() string        { return "Bash" }
func (bashImpl) Description() string { return "Execute a shell command. Output is captured and returned." }
func (bashImpl) Mutating() bool      { return true }
func (bashImpl) InputSchema() []byte {
	return []byte(`{
  "type": "object",
  "properties": {
    "cmd":         {"type": "string", "description": "Shell command to run via /bin/sh -c"},
    "cwd":         {"type": "string", "description": "Working directory (default: harness CWD)"},
    "timeout_ms":  {"type": "integer", "description": "Timeout in milliseconds (default 120000; max 600000)"},
    "env":         {"type": "object", "description": "Additional environment variables", "additionalProperties": {"type": "string"}}
  },
  "required": ["cmd"],
  "additionalProperties": false
}`)
}

type bashArgs struct {
	Cmd       string            `json:"cmd"`
	Cwd       string            `json:"cwd,omitempty"`
	TimeoutMs int               `json:"timeout_ms,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
}

func (b bashImpl) Run(ctx context.Context, call tool.ToolCall, sink tool.EventSink) (*tool.ToolResult, error) {
	var args bashArgs
	if err := json.Unmarshal(call.Input, &args); err != nil {
		return nil, fmt.Errorf("Bash: invalid arguments: %w", err)
	}
	if args.Cmd == "" {
		return nil, errors.New("Bash: cmd is required")
	}

	// Blocklist check. Scan the leading binary name of every
	// pipeline/separator-split segment, normalized via filepath.Base so
	// an absolute path (`/usr/bin/security`) matches its bare name. The
	// user is welcome to pass arguments to non-blocked commands. This is
	// defense in depth behind the authoritative permission middleware.
	blocklist := append(DefaultBashBlocklist, b.ExtraBlocklist...)
	for _, leading := range segmentCommands(args.Cmd) {
		base := filepath.Base(leading)
		for _, banned := range blocklist {
			if leading == banned || base == banned {
				return errorResult(fmt.Sprintf(
					"Bash: command %q is blocked by default. Override at the permission preset if intentional.",
					banned,
				)), nil
			}
		}
	}

	timeout := time.Duration(args.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	if timeout > 10*time.Minute {
		timeout = 10 * time.Minute
	}

	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "/bin/sh", "-c", args.Cmd)
	if args.Cwd != "" {
		cmd.Dir = args.Cwd
	}
	// Merge supplemental env onto inherited.
	if len(args.Env) > 0 {
		cmd.Env = append(cmd.Environ(), envSlice(args.Env)...)
	}
	if b.SandboxFn != nil {
		cmd = b.SandboxFn(cmd)
	}

	var out bytes.Buffer
	cmd.Stdout = &cappedWriter{w: &out, max: 1 << 20}
	cmd.Stderr = cmd.Stdout

	start := time.Now()
	err := cmd.Run()
	dur := time.Since(start)

	if cmdCtx.Err() == context.DeadlineExceeded {
		return errorResult(fmt.Sprintf(
			"Bash: command exceeded timeout %s.\nPartial output:\n%s",
			timeout, out.String(),
		)), nil
	}
	if ctx.Err() != nil {
		// Caller-cancelled (e.g. CancelTurn mid-command). Per the
		// doc's BashCancelledMidCommand error, surface a structured
		// message about possible side effects.
		return errorResult(fmt.Sprintf(
			"Bash: cancelled mid-command after %s. The command may have already modified files.\nPartial output:\n%s",
			dur, out.String(),
		)), nil
	}
	if err != nil {
		exitMsg := err.Error()
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitMsg = fmt.Sprintf("exit code %d", exitErr.ExitCode())
		}
		return errorResult(fmt.Sprintf(
			"Bash: %s\nOutput:\n%s", exitMsg, out.String(),
		)), nil
	}
	return textResult(out.String()), nil
}

func leadingCommand(cmd string) string {
	// Strip leading shell pipelines / subshells loosely; just take
	// the first whitespace-delimited token of the trimmed string.
	trimmed := strings.TrimLeft(cmd, " \t\n;|&(")
	for i, r := range trimmed {
		if r == ' ' || r == '\t' || r == '\n' {
			return trimmed[:i]
		}
	}
	return trimmed
}

// segmentCommands splits a command line on shell separators
// (`;`, `|`, `&`, newline, subshell `(` `)`, and command-substitution
// boundaries backtick / `$(` `)`) and returns the leading command
// token of each segment. It peels off env-assignment and pass-through
// prefixes (`command`, `env`, `VAR=val`), strips surrounding quotes
// and leading backslashes, and collapses intra-token quote-splitting
// (`sec''urity`) so a banned tool hidden behind quoting or substitution
// is still detected. This is a blocklist heuristic, not a shell parser
// — the permission middleware remains the authoritative gate.
func segmentCommands(cmd string) []string {
	// Treat command-substitution markers as plain separators: replace
	// backticks with a separator and the `$(` opener with `(` so the
	// existing FieldsFunc split on `(`/`)` peels the inner command into
	// its own segment. This is intentionally syntactic, not a parser.
	cmd = strings.ReplaceAll(cmd, "`", "\n")
	cmd = strings.ReplaceAll(cmd, "$(", "(")

	segs := strings.FieldsFunc(cmd, func(r rune) bool {
		switch r {
		case ';', '|', '&', '\n', '(', ')':
			return true
		}
		return false
	})
	out := make([]string, 0, len(segs))
	for _, seg := range segs {
		fields := strings.Fields(seg)
		// Skip env-assignment and pass-through prefixes to reach the
		// real command name.
		for len(fields) > 0 {
			f := normalizeToken(fields[0])
			if f == "command" || f == "env" || f == "exec" || f == "nohup" || f == "sudo" {
				fields = fields[1:]
				continue
			}
			// VAR=value assignment prefix (no slash, contains '=').
			if i := strings.IndexByte(f, '='); i > 0 && !strings.ContainsRune(f[:i], '/') {
				fields = fields[1:]
				continue
			}
			break
		}
		if len(fields) > 0 {
			out = append(out, normalizeToken(fields[0]))
		}
	}
	return out
}

// normalizeToken strips shell quoting noise from a candidate command
// token so the blocklist sees the bare name: it removes single/double
// quotes anywhere in the token (collapsing `sec''urity` → `security`),
// drops a leading backslash (`\security` → `security`), and unescapes
// backslash-escaped characters. Best-effort defense in depth.
func normalizeToken(tok string) string {
	var b strings.Builder
	b.Grow(len(tok))
	escaped := false
	for _, r := range tok {
		if escaped {
			b.WriteRune(r)
			escaped = false
			continue
		}
		switch r {
		case '\\':
			escaped = true // drop the backslash, keep the next rune literally
		case '\'', '"':
			// drop quote characters (collapses quote-splitting)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func envSlice(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	return out
}

// cappedWriter discards writes after max bytes.
type cappedWriter struct {
	w   interface{ Write([]byte) (int, error) }
	max int
	n   int
}

func (c *cappedWriter) Write(p []byte) (int, error) {
	if c.n >= c.max {
		return len(p), nil // pretend we wrote (don't error the process)
	}
	remaining := c.max - c.n
	if len(p) > remaining {
		_, _ = c.w.Write(p[:remaining])
		_, _ = c.w.Write([]byte("\n[output truncated]\n"))
		c.n = c.max
		return len(p), nil
	}
	n, err := c.w.Write(p)
	c.n += n
	return n, err
}
