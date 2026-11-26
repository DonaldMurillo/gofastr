package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"io"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

var currentOS = runtime.GOOS

type manager interface {
	kind() string
	install(b *Battery, exe string, port int) error
	uninstall(b *Battery) error
	start(b *Battery) error
	stop(b *Battery) error
	restart(b *Battery) error
	installed(b *Battery) (bool, error)
	logs(b *Battery, n int, follow bool) error
}

func managerFor(goos string) manager {
	switch goos {
	case "darwin":
		return launchd{}
	case "linux":
		return systemdUser{}
	case "windows":
		return taskScheduler{}
	}
	return unsupported{goos: goos}
}

type unsupported struct{ goos string }

func (u unsupported) kind() string { return "none" }
func (u unsupported) err() error {
	return fmt.Errorf("service: installing a service is not supported on %s; run the app in the foreground instead", u.goos)
}
func (u unsupported) install(*Battery, string, int) error { return u.err() }
func (u unsupported) uninstall(*Battery) error            { return u.err() }
func (u unsupported) start(*Battery) error                { return u.err() }
func (u unsupported) stop(*Battery) error                 { return u.err() }
func (u unsupported) restart(*Battery) error              { return u.err() }
func (u unsupported) installed(*Battery) (bool, error)    { return false, nil }
func (u unsupported) logs(*Battery, int, bool) error      { return u.err() }

type toolRunner interface {
	// output runs a tool and returns its stdout, or stdout then stderr
	// when it fails, so the error can quote the tool.
	output(name string, args ...string) ([]byte, error)
	stream(w io.Writer, name string, args ...string) error
}

const toolTimeout = 30 * time.Second

type execTools struct{}

var execTool toolRunner = execTools{}

func (execTools) output(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), toolTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = 2 * time.Second
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return append(out.Bytes(), stderr.Bytes()...), err
	}
	return out.Bytes(), nil
}

func (execTools) stream(w io.Writer, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = w
	cmd.Stderr = w
	return cmd.Run()
}

func (b *Battery) run(name string, args ...string) error {
	out, err := b.tool.output(name, args...)
	if err != nil {
		return toolError(name, args, out, err)
	}
	return nil
}

func toolError(name string, args []string, out []byte, err error) error {
	msg := strings.TrimSpace(textsafe.ScrubTerminal(string(out)))
	if msg == "" {
		return fmt.Errorf("service: %s %s: %w", name, strings.Join(args, " "), err)
	}
	return fmt.Errorf("service: %s %s: %w: %s", name, strings.Join(args, " "), err, msg)
}

func exitCode(err error) int {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}
