// Package a holds the nowaitdelay fixtures reduced from the real
// sites: cmd/kiln agent_watcher.go runOneAgentTurn (the oracle), the
// three fix spellings (inline WaitDelay, the evals helper posture, the
// *os.File stdout), and the quiet postures.
package a

import (
	"bytes"
	"context"
	"log"
	"os"
	"os/exec"
	"time"
)

// oracleTurn is cmd/kiln agent_watcher.go runOneAgentTurn:
// CommandContext, Stderr inherited, Output() — the pipe copier with no
// bound.
func oracleTurn(ctx context.Context, argv []string) (string, error) {
	c := exec.CommandContext(ctx, argv[0], argv[1:]...)
	c.Stderr = os.Stderr
	out, err := c.Output() // want `nowaitdelay: exec\.CommandContext child with captured stdout starts with no WaitDelay`
	return string(out), err
}

// chainedVersion is the evals version-check spelling: no local to
// bound.
func chainedVersion(ctx context.Context, program string) (string, error) {
	out, err := exec.CommandContext(ctx, program, "--version").CombinedOutput() // want `nowaitdelay: exec\.CommandContext\(\.\.\.\)\.CombinedOutput captures stdout with no WaitDelay`
	return string(out), err
}

// bufferStdout: Stdout assigned a buffer, started with Run.
func bufferStdout(ctx context.Context, prog string, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, prog, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil { // want `nowaitdelay: exec\.CommandContext child with captured stdout starts with no WaitDelay`
		return "", err
	}
	return out.String(), nil
}

// stdoutPipeStart: StdoutPipe + Start (the mcpclient shape).
func stdoutPipeStart(ctx context.Context, prog string) error {
	c := exec.CommandContext(ctx, prog)
	stdout, err := c.StdoutPipe()
	if err != nil {
		return err
	}
	if err := c.Start(); err != nil { // want `nowaitdelay: exec\.CommandContext child with captured stdout starts with no WaitDelay`
		return err
	}
	_ = stdout
	return c.Wait()
}

// ---- fix spellings (quiet) ---------------------------------------------

// inlineDelay is codegen/extension_command.go: WaitDelay set inline
// before the start.
func inlineDelay(ctx context.Context, prog string, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, prog, args...)
	cmd.Dir = "."
	cmd.WaitDelay = 5 * time.Second
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	runErr := cmd.Run()
	_ = stderr
	return stdout.String(), runErr
}

// helperDelay is the evals posture: a same-package helper bounds the
// child.
func helperDelay(ctx context.Context, prog string, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, prog, args...)
	configureCommandCancellation(cmd)
	cmd.Dir = "."
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	err := cmd.Run()
	return output.String(), err
}

func configureCommandCancellation(cmd *exec.Cmd) {
	cmd.WaitDelay = 5 * time.Second
	cmd.SysProcAttr = nil
}

// fileStdout is the codex.go runCodex shape: Stdout is a log FILE —
// no copier, no pipe.
func fileStdout(ctx context.Context, prog string, args []string) error {
	logFile, err := os.OpenFile("child.log", os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, prog, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	return cmd.Run()
}

// inheritStdout: Stdout never set — the child writes to the parent's
// terminal fd directly.
func inheritStdout(ctx context.Context, prog string, args []string) error {
	cmd := exec.CommandContext(ctx, prog, args...)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// noContextCommand is framework/processmodule_probe.go's runner: no
// context, no cancellation to bound.
func noContextCommand(exe string) ([]byte, error) {
	cmd := exec.Command(exe)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return out.Bytes(), cmd.Wait()
}

// wrapperReassign is bash.go's SandboxFn posture: the Cmd handed to
// and returned from a wrapper is the wrapper's business.
func wrapperReassign(ctx context.Context, script string) error {
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", script)
	cmd = wrapForSandbox(cmd)
	var out bytes.Buffer
	cmd.Stdout = &cappedWriter{w: &out}
	cmd.Stderr = cmd.Stdout
	return cmd.Run()
}

func wrapForSandbox(cmd *exec.Cmd) *exec.Cmd {
	// The bash.go SandboxFn posture: wraps for OS sandboxing and
	// returns the Cmd; whether it bounds Wait is invisible here.
	cmd.SysProcAttr = nil
	return cmd
}

type cappedWriter struct {
	w *bytes.Buffer
}

func (c *cappedWriter) Write(p []byte) (int, error) { return c.w.Write(p) }

// neverStarted: the Cmd is built and configured but started elsewhere
// (returned to the caller).
func neverStarted(ctx context.Context, prog string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, prog)
	var out bytes.Buffer
	cmd.Stdout = &out
	return cmd
}

// stderrOnlyCap: only Stderr is captured; stdout has no copier. Not
// this rule's shape.
func stderrOnlyCap(ctx context.Context, prog string) error {
	cmd := exec.CommandContext(ctx, prog)
	var out bytes.Buffer
	cmd.Stderr = &out
	return cmd.Run()
}

// stdLogSink proves the log import is used by the fixture build (the
// recoverlog twin owns log sinks; this rule has none).
var _ = log.Print
