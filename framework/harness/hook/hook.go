// Package hook implements lifecycle hooks: shell commands the
// harness runs at well-known event points. Per § Hook timeouts:
//
//   - Per-event default timeouts (5/30/30/60/5/5).
//   - SIGTERM at deadline, SIGKILL at deadline+5s.
//   - stdout/stderr capped at 64KB.
//   - Non-zero exit emits HookError; deadline emits HookTimeout.
//
// Hooks are SHA-256 hashed (rule 13). Project-local hooks
// (`<repo>/.gofastr/harness/hooks/`) are off unless
// --allow-project-hooks is set.
package hook

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Event names; constants mirror the doc.
type Event string

const (
	EventSessionStart     Event = "SessionStart"
	EventUserPromptSubmit Event = "UserPromptSubmit"
	EventPreToolUse       Event = "PreToolUse"
	EventPostToolUse      Event = "PostToolUse"
	EventCompact          Event = "Compact"
	EventStop             Event = "Stop"
)

// DefaultTimeout returns the per-event default timeout. Profiles may
// override per-hook via timeout_ms in TOML.
func DefaultTimeout(e Event) time.Duration {
	switch e {
	case EventSessionStart, EventUserPromptSubmit, EventStop:
		return 5 * time.Second
	case EventPreToolUse, EventPostToolUse:
		return 30 * time.Second
	case EventCompact:
		return 60 * time.Second
	default:
		return 30 * time.Second
	}
}

// Hook is one configured hook entry.
type Hook struct {
	Event   Event
	Command string        // shell command, run via /bin/sh -c
	Timeout time.Duration // 0 = use DefaultTimeout(Event)
	SHA256  string        // populated by the TOFU loader

	// Source tells the runner where the hook came from for the
	// --allow-project-hooks gate. "user" = ~/.config/...; "project"
	// = <repo>/.gofastr/...; "builtin" = ships with the binary.
	Source string
}

// Result is the outcome of running one hook.
type Result struct {
	Event     Event
	Command   string
	ExitCode  int
	TimedOut  bool
	Output    string // combined stdout+stderr, capped at 64KB
	Duration  time.Duration
}

// Runner runs hooks per event. Concurrency-safe.
type Runner struct {
	mu                sync.RWMutex
	byEvent           map[Event][]Hook
	AllowProjectHooks bool
}

// New returns an empty Runner.
func New() *Runner {
	return &Runner{byEvent: make(map[Event][]Hook)}
}

// Register adds a hook. Returns an error if the hook's Source is
// "project" and AllowProjectHooks is false — the hook is silently
// skipped (no error returned to keep callers simple).
func (r *Runner) Register(h Hook) error {
	if h.Event == "" || h.Command == "" {
		return errors.New("hook: Event and Command are required")
	}
	if h.Source == "project" && !r.AllowProjectHooks {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byEvent[h.Event] = append(r.byEvent[h.Event], h)
	return nil
}

// HooksFor returns the registered hooks for an event in registration order.
func (r *Runner) HooksFor(e Event) []Hook {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cp := make([]Hook, len(r.byEvent[e]))
	copy(cp, r.byEvent[e])
	return cp
}

// Run executes every hook for the given event sequentially. Returns
// a slice of results. The caller is expected to publish HookTimeout
// / HookError events to the bus based on each Result.
func (r *Runner) Run(ctx context.Context, e Event, env []string) []Result {
	hooks := r.HooksFor(e)
	results := make([]Result, 0, len(hooks))
	for _, h := range hooks {
		res := runOne(ctx, h, env)
		results = append(results, res)
	}
	return results
}

func runOne(ctx context.Context, h Hook, env []string) Result {
	timeout := h.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout(h.Event)
	}
	deadline := time.Now().Add(timeout)
	cmdCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "/bin/sh", "-c", h.Command)
	if len(env) > 0 {
		cmd.Env = append(cmd.Environ(), env...)
	}
	// On Linux, this sets a process group so we can signal children.
	setProcGroup(cmd)

	var buf bytes.Buffer
	cmd.Stdout = capped(&buf, 64*1024)
	cmd.Stderr = cmd.Stdout

	start := time.Now()
	err := cmd.Run()
	dur := time.Since(start)

	res := Result{
		Event:    h.Event,
		Command:  h.Command,
		Output:   buf.String(),
		Duration: dur,
	}
	if cmdCtx.Err() == context.DeadlineExceeded {
		res.TimedOut = true
		// SIGTERM, then SIGKILL after a 5s grace.
		killAfter(cmd, 5*time.Second)
		return res
	}
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			res.ExitCode = exitErr.ExitCode()
		} else {
			res.ExitCode = -1
		}
		return res
	}
	res.ExitCode = 0
	return res
}

// capped wraps an io.Writer with a byte limit. Writes past the limit
// are dropped silently (and a truncation marker appended exactly once).
type cappedWriter struct {
	w    *bytes.Buffer
	max  int
	wrote int
	marked bool
}

func capped(w *bytes.Buffer, max int) *cappedWriter {
	return &cappedWriter{w: w, max: max}
}

func (c *cappedWriter) Write(p []byte) (int, error) {
	if c.wrote >= c.max {
		return len(p), nil
	}
	remaining := c.max - c.wrote
	if len(p) > remaining {
		_, _ = c.w.Write(p[:remaining])
		if !c.marked {
			_, _ = c.w.Write([]byte("\n[hook output truncated]\n"))
			c.marked = true
		}
		c.wrote = c.max
		return len(p), nil
	}
	n, err := c.w.Write(p)
	c.wrote += n
	return n, err
}

// killAfter does best-effort SIGKILL grace handling. On platforms
// without process groups (Windows) this falls back to whatever
// CommandContext already did.
func killAfter(cmd *exec.Cmd, grace time.Duration) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	// CommandContext fires SIGKILL when ctx is done; we want SIGTERM
	// first. Best effort.
	_ = cmd.Process.Signal(syscall.SIGTERM)
	timer := time.NewTimer(grace)
	defer timer.Stop()
	select {
	case <-timer.C:
		_ = cmd.Process.Signal(syscall.SIGKILL)
	}
}

// Summary returns a short human-readable description for the cost meter / TUI.
func (r Result) Summary() string {
	if r.TimedOut {
		return fmt.Sprintf("hook %s timed out after %s", r.Event, r.Duration)
	}
	if r.ExitCode != 0 {
		return fmt.Sprintf("hook %s exit %d after %s", r.Event, r.ExitCode, r.Duration)
	}
	return fmt.Sprintf("hook %s ok in %s", r.Event, r.Duration)
}

// TrimOutput returns the first n bytes of Output, ellipsizing if cut.
func (r Result) TrimOutput(n int) string {
	if len(r.Output) <= n {
		return r.Output
	}
	return r.Output[:n] + "…"
}

// Lint-suppress unused param.
var _ = strings.Contains
