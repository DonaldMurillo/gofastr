package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// currentOS is runtime.GOOS, a variable so tests can drive every
// backend's argv and templates from one machine.
var currentOS = runtime.GOOS

// manager is one OS service manager's per-user backend. Every method
// is idempotent where the verb allows it: install over an existing
// install replaces the definition and restarts, uninstall of something
// not installed succeeds.
type manager interface {
	// kind names the manager in CLI output.
	kind() string
	// install writes the definition for exe and registers and starts it.
	install(b *Battery, exe string, port int) error
	// uninstall stops, unregisters and removes the definition.
	uninstall(b *Battery) error
	start(b *Battery) error
	stop(b *Battery) error
	restart(b *Battery) error
	// installed reports whether a definition is registered.
	installed(b *Battery) (bool, error)
	// logs prints the last n lines and, with follow, keeps printing.
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

// unsupported answers every verb with the same named refusal.
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

// toolRunner runs the service manager's CLI tools. Every external
// command goes through it, so tests assert the exact argv without
// launchctl, systemctl or schtasks being real.
type toolRunner interface {
	// output runs name with args and returns combined output.
	output(name string, args ...string) ([]byte, error)
	// stream runs name with args, copying its output to w until it
	// exits (journalctl -f).
	stream(w io.Writer, name string, args ...string) error
}

// toolTimeout bounds one manager call. launchctl bootstrap and
// systemctl enable return in well under a second; a call that hangs is
// a wedged manager, not a slow one.
const toolTimeout = 30 * time.Second

type execTools struct{}

var execTool toolRunner = execTools{}

func (execTools) output(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), toolTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = 2 * time.Second
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.Bytes(), err
}

func (execTools) stream(w io.Writer, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = w
	cmd.Stderr = w
	return cmd.Run()
}

// run is output with the tool's own words folded into the error, so a
// refusal from launchctl or systemctl reaches the user verbatim
// (control bytes stripped: the text goes to a terminal).
func (b *Battery) run(name string, args ...string) error {
	out, err := b.tool.output(name, args...)
	if err != nil {
		return toolError(name, args, out, err)
	}
	return nil
}

func toolError(name string, args []string, out []byte, err error) error {
	msg := strings.TrimSpace(scrubControl(string(out)))
	if msg == "" {
		return fmt.Errorf("service: %s %s: %w", name, strings.Join(args, " "), err)
	}
	return fmt.Errorf("service: %s %s: %w: %s", name, strings.Join(args, " "), err, msg)
}

// scrubControl drops C0 control bytes and DEL except newline and tab,
// so tool output and log lines cannot drive the user's terminal.
func scrubControl(s string) string {
	return strings.Map(func(r rune) rune {
		if (r < 0x20 && r != '\n' && r != '\t') || r == 0x7f {
			return -1
		}
		return r
	}, s)
}

// exitCode returns the tool's exit status, or -1 when err is not an
// exit (the tool did not run at all).
func exitCode(err error) int {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}
