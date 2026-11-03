package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// runner executes (or, with dryRun, only prints) the build's steps. Every
// external command goes through it, so --dry-run shows exactly what a real
// run would execute, in order, and a failure names the step it broke.
type runner struct {
	ctx    context.Context
	out    io.Writer
	dryRun bool
	// env is appended to the process environment for every command
	// (PATH with the pinned gomobile/gobind first, the Android SDK vars).
	env  []string
	step int
}

func (r *runner) header(desc string) {
	r.step++
	fmt.Fprintf(r.out, "\n[%d] %s\n", r.step, desc)
}

func (r *runner) command(name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(r.ctx, name, args...)
	cmd.Env = append(os.Environ(), r.env...)
	// A child that keeps its pipes open after exit (a simulator helper,
	// an emulator it forked) must not wedge Wait.
	cmd.WaitDelay = 5 * time.Second
	return cmd
}

// run executes one step, streaming its output.
func (r *runner) run(desc, name string, args ...string) error {
	r.header(desc)
	fmt.Fprintf(r.out, "    $ %s\n", shellJoin(name, args))
	if r.dryRun {
		return nil
	}
	cmd := r.command(name, args...)
	cmd.Stdout = r.out
	cmd.Stderr = r.out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("step %d (%s): %w", r.step, desc, err)
	}
	return nil
}

// tolerate runs a step whose failure is expected and harmless (booting a
// simulator that is already booted).
func (r *runner) tolerate(desc, name string, args ...string) {
	if err := r.run(desc, name, args...); err != nil {
		fmt.Fprintf(r.out, "    (ignored: %v)\n", err)
	}
}

// output runs a step and returns its stdout. In a dry run nothing runs and
// placeholder stands in for the answer.
func (r *runner) output(desc, placeholder, name string, args ...string) (string, error) {
	b, err := r.outputBytes(desc, []byte(placeholder), name, args...)
	return strings.TrimSpace(string(b)), err
}

func (r *runner) outputBytes(desc string, placeholder []byte, name string, args ...string) ([]byte, error) {
	r.header(desc)
	fmt.Fprintf(r.out, "    $ %s\n", shellJoin(name, args))
	if r.dryRun {
		return placeholder, nil
	}
	cmd := r.command(name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("step %d (%s): %w\n%s", r.step, desc, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// do runs an in-process step (writing a file, repacking a zip).
func (r *runner) do(desc string, fn func() error) error {
	r.header(desc)
	if r.dryRun {
		return nil
	}
	if err := fn(); err != nil {
		return fmt.Errorf("step %d (%s): %w", r.step, desc, err)
	}
	return nil
}

func (r *runner) sleep(d time.Duration, why string) {
	r.header(fmt.Sprintf("wait %s %s", d, why))
	if r.dryRun {
		return
	}
	select {
	case <-time.After(d):
	case <-r.ctx.Done():
	}
}

// shellJoin renders a command the way a person would paste it.
func shellJoin(name string, args []string) string {
	parts := make([]string, 0, len(args)+1)
	for _, a := range append([]string{name}, args...) {
		parts = append(parts, shellQuote(a))
	}
	return strings.Join(parts, " ")
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if strings.IndexFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_./=:,+@%", r))
	}) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
