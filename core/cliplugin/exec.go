package cliplugin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"time"
)

// ExecGracePeriod is how long Run waits for a plugin process to exit cleanly
// after an interrupt/cancellation before terminating it.
// Var so tests can tune it deterministically.
var ExecGracePeriod = 5 * time.Second

// ExecRequest describes one plugin invocation.
type ExecRequest struct {
	PluginName    string
	Command       []string // resolved (or verified snapshot) command vector
	Args          []string // user arguments forwarded verbatim without shell interpolation
	WorkDir       string
	CLIVersion    string
	CLIBin        string
	ProjectRoot   string
	Mode          InvocationMode
	HookPhase     HookPhase
	TargetCommand string
	AllowEnv      []string
	AllowEnvFlags []string
	Environ       []string // optional source env; defaults to os.Environ()
	Stdin         io.Reader
	Stdout        io.Writer
	Stderr        io.Writer
}

// Run executes a plugin binary directly (no shell), streaming stdin/stdout/stderr,
// forwarding OS interrupts/cancellation cleanly with bounded grace, and
// returning the child's exit code. A non-nil error indicates launch failure
// rather than a normal non-zero plugin exit code.
func Run(ctx context.Context, req ExecRequest) (int, error) {
	if len(req.Command) == 0 || req.Command[0] == "" {
		return 1, fmt.Errorf("%w: empty plugin command", ErrInvalidPath)
	}
	if ctx.Err() != nil {
		return 130, nil
	}

	argv := make([]string, 0, len(req.Command)-1+len(req.Args))
	argv = append(argv, req.Command[1:]...)
	argv = append(argv, req.Args...)

	srcEnv := req.Environ
	if srcEnv == nil {
		srcEnv = os.Environ()
	}

	protoVars := map[string]string{
		EnvProtocolVersion: fmt.Sprintf("%d", ProtocolVersion),
		EnvCLIVersion:      req.CLIVersion,
		EnvCLIBin:          req.CLIBin,
		EnvPluginName:      req.PluginName,
		EnvProjectRoot:     req.ProjectRoot,
	}
	switch req.Mode {
	case ModeHook:
		protoVars[EnvHookPhase] = string(req.HookPhase)
		protoVars[EnvHookCommand] = req.TargetCommand
	case ModeOverride:
		protoVars[EnvOverrideCommand] = req.TargetCommand
		if hasNonEmptyEnv(srcEnv, "DATABASE_URL") {
			protoVars[EnvFilteredDB] = "1"
		}
	}

	cmd := exec.Command(req.Command[0], argv...)
	if req.WorkDir != "" {
		cmd.Dir = req.WorkDir
	}
	cmd.Env = BuildExecEnv(EnvOptions{
		Environ:      srcEnv,
		AllowEnv:     req.AllowEnv,
		AllowEnvFlag: req.AllowEnvFlags,
		Args:         req.Args,
		ProtocolVars: protoVars,
	})
	cmd.Stdin = req.Stdin
	cmd.Stdout = req.Stdout
	cmd.Stderr = req.Stderr
	if cmd.Stdin == nil {
		cmd.Stdin = os.Stdin
	}
	if cmd.Stdout == nil {
		cmd.Stdout = os.Stdout
	}
	if cmd.Stderr == nil {
		cmd.Stderr = os.Stderr
	}
	cmd.WaitDelay = ExecGracePeriod

	isolatedPgrp := configureProcessGroup(cmd, isTTYReader(cmd.Stdin))

	if err := cmd.Start(); err != nil {
		return 1, fmt.Errorf("start plugin %q: %w", req.PluginName, err)
	}

	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, forwardedSignals()...)
	defer signal.Stop(sigCh)

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	interrupted := false
	var graceTimer *time.Timer
	var graceC <-chan time.Time
	defer func() {
		if graceTimer != nil {
			graceTimer.Stop()
		}
	}()

	beginShutdown := func(sig os.Signal, fromTerminal bool) {
		if !interrupted {
			interrupted = true
			if cmd.Process != nil {
				// When the child shares the terminal's foreground process
				// group, the OS console/TTY driver already delivered Ctrl+C
				// directly to the child; only arm the grace timer instead of
				// sending a duplicate SIGINT.
				if !(fromTerminal && !isolatedPgrp && sig == os.Interrupt) {
					_ = signalChildTree(cmd.Process, sig, isolatedPgrp)
				}
			}
			graceTimer = time.NewTimer(ExecGracePeriod)
			graceC = graceTimer.C
		} else if cmd.Process != nil {
			// Second interrupt escalates immediately to forceful termination.
			_ = killChildTree(cmd.Process, isolatedPgrp)
		}
	}

	ctxDone := ctx.Done()
	for {
		select {
		case err := <-waitCh:
			return exitCodeFromWait(err, interrupted), nil
		case sig := <-sigCh:
			beginShutdown(sig, true)
		case <-ctxDone:
			ctxDone = nil // consume cancellation once so the grace period runs
			beginShutdown(os.Interrupt, false)
		case <-graceC:
			if cmd.Process != nil {
				_ = killChildTree(cmd.Process, isolatedPgrp)
			}
			graceC = nil
		}
	}
}

func isTTYReader(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok || f == nil {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

func hasNonEmptyEnv(environ []string, name string) bool {
	for _, kv := range environ {
		k, v, ok := strings.Cut(kv, "=")
		if ok && strings.EqualFold(k, name) && strings.TrimSpace(v) != "" {
			return true
		}
	}
	return false
}

func exitCodeFromWait(err error, interrupted bool) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if interrupted {
			code := exitErr.ExitCode()
			if code <= 0 {
				return 130
			}
			// Preserve an explicit plugin interrupt exit code (e.g. 130) or
			// normalize forceful termination on Windows (which yields 1).
			if code == 1 {
				return 130
			}
			return code
		}
		code := exitErr.ExitCode()
		if code >= 0 {
			return code
		}
		return 1
	}
	if interrupted {
		return 130
	}
	return 1
}
