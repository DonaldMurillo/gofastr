package service

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/DonaldMurillo/gofastr/internal/fileperm"
)

// launchd is the macOS backend: a LaunchAgent plist in
// ~/Library/LaunchAgents, bootstrapped into the gui/<uid> domain with
// the modern launchctl verbs (bootstrap, bootout, kickstart, kill).
// `launchctl print` output is documented as unstable, so status never
// parses it; the daemon answers status itself.
type launchd struct{}

func (launchd) kind() string { return "launchd" }

func (launchd) plistPath(b *Battery) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("service: resolve home dir: %w", err)
	}
	return filepath.Join(home, "Library", "LaunchAgents", b.cfg.ID+".plist"), nil
}

// currentUID is os.Getuid, a variable so tests pin the launchd domain.
var currentUID = os.Getuid

func (launchd) domain() string { return "gui/" + strconv.Itoa(currentUID()) }

func (l launchd) target(b *Battery) string { return l.domain() + "/" + b.cfg.ID }

// plist renders the agent definition. KeepAlive restarts a crash but
// leaves a clean exit down, which is what `stop` relies on: SIGTERM,
// graceful drain, exit 0, stays stopped until start or next login.
// Output and errors go to the state directory's log, which the daemon
// trims at start (launchd does not rotate).
func (launchd) plist(b *Battery, args []string, logPath string) []byte {
	var buf bytes.Buffer
	str := func(s string) {
		buf.WriteString("<string>")
		_ = xml.EscapeText(&buf, []byte(s))
		buf.WriteString("</string>")
	}
	buf.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	`)
	str(b.cfg.ID)
	buf.WriteString("\n\t<key>ProgramArguments</key>\n\t<array>\n")
	for _, a := range args {
		buf.WriteString("\t\t")
		str(a)
		buf.WriteString("\n")
	}
	buf.WriteString("\t</array>\n\t<key>RunAtLoad</key>\n\t<true/>\n")
	buf.WriteString("\t<key>KeepAlive</key>\n\t<dict>\n\t\t<key>SuccessfulExit</key>\n\t\t<false/>\n\t</dict>\n")
	buf.WriteString("\t<key>ProcessType</key>\n\t<string>Background</string>\n")
	buf.WriteString("\t<key>StandardOutPath</key>\n\t")
	str(logPath)
	buf.WriteString("\n\t<key>StandardErrorPath</key>\n\t")
	str(logPath)
	buf.WriteString("\n</dict>\n</plist>\n")
	return buf.Bytes()
}

func (l launchd) install(b *Battery, exe string, port int) error {
	path, err := l.plistPath(b)
	if err != nil {
		return err
	}
	inst, err := b.layoutFor(false)
	if err != nil {
		return err
	}
	if err := ensurePrivateDir(inst.logDir()); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("service: create %s: %w", filepath.Dir(path), err)
	}
	args, err := b.daemonArgv(exe, port)
	if err != nil {
		return err
	}
	// Owner-only: launchd reads a per-user agent as that user.
	if err := fileperm.WriteOwnerOnly(path, l.plist(b, args, inst.logPath())); err != nil {
		return fmt.Errorf("service: write %s: %w", path, err)
	}
	// Replace a loaded copy: bootout fails harmlessly when nothing is
	// loaded, and bootstrap refuses a label that is still loaded.
	_, _ = b.tool.output("launchctl", "bootout", l.target(b))
	_ = b.run("launchctl", "enable", l.target(b))
	if err := b.run("launchctl", "bootstrap", l.domain(), path); err != nil {
		return fmt.Errorf("%w\n(the gui/<uid> domain exists only while you are logged in at the Mac's screen)", err)
	}
	return nil
}

func (l launchd) uninstall(b *Battery) error {
	path, err := l.plistPath(b)
	if err != nil {
		return err
	}
	_, _ = b.tool.output("launchctl", "bootout", l.target(b))
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("service: remove %s: %w", path, err)
	}
	return nil
}

func (l launchd) start(b *Battery) error {
	if err := b.run("launchctl", "kickstart", l.target(b)); err == nil {
		return nil
	}
	// Not loaded (booted out, or never bootstrapped since login):
	// bootstrap runs it through RunAtLoad.
	path, err := l.plistPath(b)
	if err != nil {
		return err
	}
	return b.run("launchctl", "bootstrap", l.domain(), path)
}

// stop sends SIGTERM and waits for the process to exit: the daemon
// drains, exits 0, and KeepAlive leaves it down. A daemon that is not
// running is already stopped (`launchctl kill` would fail on it).
//
// One still in its builder past startingWait is booted out instead: it
// has no SIGTERM handler until it is ready, so the signal kills it, a
// death launchd's KeepAlive {SuccessfulExit=false} counts as a crash and
// restarts. bootout unloads the job, so nothing restarts it; start
// bootstraps it again. The wait is on its control channel, since no
// reply named its pid.
func (l launchd) stop(b *Battery) error {
	daemon, err := b.runningDaemon()
	starting := errors.Is(err, errStarting)
	if err != nil && !starting {
		return err
	}
	if daemon.PID == 0 && !starting {
		return nil
	}
	if starting {
		if err := b.run("launchctl", "bootout", l.target(b)); err != nil {
			return err
		}
		return b.waitExit(daemon)
	}
	if err := b.run("launchctl", "kill", "SIGTERM", l.target(b)); err != nil {
		return err
	}
	return b.waitExit(daemon)
}

// restart is stop (which waits for the exit), then start, rather than
// `kickstart -k`: a graceful drain, then a fresh start once the old
// process has released the port and the control socket. kickstart
// without -k does nothing to a job that is still running, so the wait
// is what makes the start take.
func (l launchd) restart(b *Battery) error {
	if err := l.stop(b); err != nil {
		return err
	}
	return l.start(b)
}

func (l launchd) installed(b *Battery) (bool, error) {
	path, err := l.plistPath(b)
	if err != nil {
		return false, err
	}
	return fileExists(path), nil
}

func (launchd) logs(b *Battery, n int, follow bool) error {
	inst, err := b.layoutFor(false)
	if err != nil {
		return err
	}
	return tailFile(b.cfg.Stdout, inst.logPath(), n, follow)
}

// daemonArgs is the argv every backend registers. base is the state
// base the CLI resolved at install, passed explicitly because the
// service manager does not see the shell's environment
// (GOFASTR_SERVICE_DATA_DIR, XDG_CONFIG_HOME).
func daemonArgs(exe string, port int, base string) []string {
	return []string{exe, "service", "daemon", "--port", strconv.Itoa(port), "--base-dir", base}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
