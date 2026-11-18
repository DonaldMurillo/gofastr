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

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/tool"
)

type bashImpl struct {
	// SandboxFn, when set, wraps the command for OS-level sandboxing before it runs.
	SandboxFn func(*exec.Cmd) *exec.Cmd
	// ExtraBlocklist adds command names to DefaultBashBlocklist.
	ExtraBlocklist []string
}

// DefaultBashBlocklist names the credential-store commands Bash refuses to run.
var DefaultBashBlocklist = []string{
	"security",
	"secret-tool",
	"keyctl",
	"kwalletcli",
	"systemd-ask-password",
}

func (bashImpl) Name() string { return "Bash" }
func (bashImpl) Description() string {
	return "Execute a shell command. Output is captured and returned."
}
func (bashImpl) Mutating() bool { return true }
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

	const maxBashTimeout = 10 * time.Minute
	timeout := 2 * time.Minute
	switch {
	case args.TimeoutMs > int(maxBashTimeout/time.Millisecond):
		timeout = maxBashTimeout
	case args.TimeoutMs > 0:
		timeout = time.Duration(args.TimeoutMs) * time.Millisecond
	}

	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "/bin/sh", "-c", args.Cmd)
	if args.Cwd != "" {
		cmd.Dir = args.Cwd
	}
	if len(args.Env) > 0 {
		cmd.Env = append(cmd.Environ(), envSlice(args.Env)...)
	}
	if b.SandboxFn != nil {
		cmd = b.SandboxFn(cmd)
	}
	killGroupOnCancel(cmd)
	cmd.WaitDelay = 2 * time.Second

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
	trimmed := strings.TrimLeft(cmd, " \t\n;|&(")
	for i, r := range trimmed {
		if r == ' ' || r == '\t' || r == '\n' {
			return trimmed[:i]
		}
	}
	return trimmed
}

func segmentCommands(cmd string) []string {
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
		for len(fields) > 0 {
			f := normalizeToken(fields[0])
			if f == "command" || f == "env" || f == "exec" || f == "nohup" || f == "sudo" {
				fields = fields[1:]
				continue
			}
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
			escaped = true
		case '\'', '"':
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

type cappedWriter struct {
	w         interface{ Write([]byte) (int, error) }
	max       int
	n         int
	truncated bool
}

func (c *cappedWriter) Write(p []byte) (int, error) {
	remaining := c.max - c.n
	if len(p) <= remaining {
		n, err := c.w.Write(p)
		c.n += n
		return n, err
	}
	if remaining > 0 {
		_, _ = c.w.Write(p[:remaining])
		c.n = c.max
	}
	if !c.truncated {
		_, _ = c.w.Write([]byte("\n[output truncated]\n"))
		c.truncated = true
	}
	return len(p), nil
}
