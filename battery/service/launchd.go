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

type launchd struct{}

func (launchd) kind() string { return "launchd" }

func (launchd) plistPath(b *Battery) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("service: resolve home dir: %w", err)
	}
	return filepath.Join(home, "Library", "LaunchAgents", b.cfg.ID+".plist"), nil
}

var currentUID = os.Getuid

func (launchd) domain() string { return "gui/" + strconv.Itoa(currentUID()) }

func (l launchd) target(b *Battery) string { return l.domain() + "/" + b.cfg.ID }

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
	if err := fileperm.WriteOwnerOnly(path, l.plist(b, args, inst.logPath())); err != nil {
		return fmt.Errorf("service: write %s: %w", path, err)
	}
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
	path, err := l.plistPath(b)
	if err != nil {
		return err
	}
	return b.run("launchctl", "bootstrap", l.domain(), path)
}

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

func daemonArgs(exe string, port int, base string) []string {
	return []string{exe, "service", "daemon", "--port", strconv.Itoa(port), "--base-dir", base}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
