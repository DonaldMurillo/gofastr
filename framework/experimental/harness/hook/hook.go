package hook

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"time"
)

// Event names a point in a session where hooks run.
type Event string

const (
	EventSessionStart     Event = "SessionStart"
	EventUserPromptSubmit Event = "UserPromptSubmit"
	EventPreToolUse       Event = "PreToolUse"
	EventPostToolUse      Event = "PostToolUse"
	EventCompact          Event = "Compact"
	EventStop             Event = "Stop"
)

// DefaultTimeout returns the timeout a hook with a zero Timeout gets for event e.
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
	Timeout time.Duration // 0 = use DefaultTimeout(Event); negative is rejected by Register
	SHA256  string        // populated by the TOFU loader
	// Source is "user", "project" or "builtin"; a "project" hook needs Runner.AllowProjectHooks.
	Source string
}

// Result is the outcome of running one hook.
type Result struct {
	Event    Event
	Command  string
	ExitCode int
	TimedOut bool
	Output   string // combined stdout+stderr, capped at 64KB
	Duration time.Duration
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

// Register adds a hook, errors on an empty Event or Command or a negative Timeout, and skips a "project" hook without error unless AllowProjectHooks is set.
func (r *Runner) Register(h Hook) error {
	if h.Event == "" || h.Command == "" {
		return errors.New("hook: Event and Command are required")
	}
	if h.Timeout < 0 {
		return fmt.Errorf("hook: Timeout must be >= 0 (got %v)", h.Timeout)
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

// Run runs every hook for e in registration order and returns one Result per hook; the caller publishes HookTimeout and HookError events.
func (r *Runner) Run(ctx context.Context, e Event, env []string) []Result {
	hooks := r.HooksFor(e)
	results := make([]Result, 0, len(hooks))
	for _, h := range hooks {
		res := runOne(ctx, h, env)
		results = append(results, res)
	}
	return results
}

const killGrace = 5 * time.Second

func runOne(ctx context.Context, h Hook, env []string) Result {
	timeout := h.Timeout
	//gofastr:allow(optzero) Register rejects a negative Timeout before a hook can reach runOne
	if timeout == 0 {
		timeout = DefaultTimeout(h.Event)
	}
	deadline := time.Now().Add(timeout)
	cmdCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "/bin/sh", "-c", h.Command)
	if len(env) > 0 {
		cmd.Env = append(cmd.Environ(), env...)
	}
	setProcGroup(cmd)
	cmd.WaitDelay = killGrace

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
		killProcGroup(cmd)
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

type cappedWriter struct {
	w      *bytes.Buffer
	max    int
	wrote  int
	marked bool
}

func capped(w *bytes.Buffer, max int) *cappedWriter {
	return &cappedWriter{w: w, max: max}
}

func (c *cappedWriter) Write(p []byte) (int, error) {
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
