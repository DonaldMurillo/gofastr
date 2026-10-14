package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/internal/fileperm"
)

// systemdUser is the Linux backend: a `systemctl --user` unit. The
// unit is Type=notify, so `systemctl --user start` returns once the
// daemon has bound its port and sent READY=1, and a port collision
// fails the start instead of reporting success.
//
// A user unit runs only while the user has a session, unless lingering
// is enabled (`loginctl enable-linger`), which install prints as a hint
// rather than doing: it changes what runs at boot for the whole
// account.
type systemdUser struct{}

func (systemdUser) kind() string { return "systemd --user" }

func (systemdUser) unitName(b *Battery) string { return b.cfg.ID + ".service" }

func (s systemdUser) unitPath(b *Battery) (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" || !filepath.IsAbs(base) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("service: resolve home dir: %w", err)
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "systemd", "user", s.unitName(b)), nil
}

// systemdBooted reports whether systemd is the running init
// (sd_booted(3)); a variable so tests reach install's argv on a Mac.
var systemdBooted = func() bool { return fileExists("/run/systemd/system") }

// unit renders the definition. Output goes to the journal (stdout and
// stderr are captured by default), which `logs` reads back.
func (systemdUser) unit(b *Battery, exe string, port int) []byte {
	args := daemonArgs(exe, port)
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = quoteSystemd(a)
	}
	var sb strings.Builder
	sb.WriteString("[Unit]\n")
	sb.WriteString("Description=" + b.cfg.Name + " (" + b.cfg.ID + ")\n\n")
	sb.WriteString("[Service]\n")
	sb.WriteString("Type=notify\n")
	sb.WriteString("NotifyAccess=main\n")
	sb.WriteString("ExecStart=" + strings.Join(quoted, " ") + "\n")
	sb.WriteString("Restart=on-failure\n")
	sb.WriteString("RestartSec=2\n")
	sb.WriteString("TimeoutStopSec=20\n\n")
	sb.WriteString("[Install]\n")
	sb.WriteString("WantedBy=default.target\n")
	return []byte(sb.String())
}

// quoteSystemd quotes one ExecStart word: double quotes, with
// backslash and double quote escaped, "%" doubled (specifier
// expansion) and "$" doubled (environment expansion).
func quoteSystemd(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "%", "%%", "$", "$$")
	return `"` + r.Replace(s) + `"`
}

func (s systemdUser) install(b *Battery, exe string, port int) error {
	if !systemdBooted() {
		return fmt.Errorf("service: systemd is not running on this machine; run `%s service daemon --port %d` under your own supervisor", exe, port)
	}
	path, err := s.unitPath(b)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("service: create %s: %w", filepath.Dir(path), err)
	}
	if err := fileperm.WriteOwnerOnly(path, s.unit(b, exe, port)); err != nil {
		return fmt.Errorf("service: write %s: %w", path, err)
	}
	if err := b.run("systemctl", "--user", "daemon-reload"); err != nil {
		return err
	}
	if err := b.run("systemctl", "--user", "enable", s.unitName(b)); err != nil {
		return err
	}
	// restart starts a stopped unit and replaces a running one, so a
	// reinstall picks up the new binary.
	return b.run("systemctl", "--user", "restart", s.unitName(b))
}

func (s systemdUser) uninstall(b *Battery) error {
	path, err := s.unitPath(b)
	if err != nil {
		return err
	}
	_, _ = b.tool.output("systemctl", "--user", "disable", "--now", s.unitName(b))
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("service: remove %s: %w", path, err)
	}
	// A drop-in directory left behind would re-apply to a later
	// install of the same id (kardianos/service #412).
	if err := os.RemoveAll(path + ".d"); err != nil {
		return fmt.Errorf("service: remove %s.d: %w", path, err)
	}
	_, _ = b.tool.output("systemctl", "--user", "daemon-reload")
	return nil
}

func (s systemdUser) start(b *Battery) error {
	return b.run("systemctl", "--user", "start", s.unitName(b))
}

func (s systemdUser) stop(b *Battery) error {
	return b.run("systemctl", "--user", "stop", s.unitName(b))
}

func (s systemdUser) restart(b *Battery) error {
	return b.run("systemctl", "--user", "restart", s.unitName(b))
}

func (s systemdUser) installed(b *Battery) (bool, error) {
	path, err := s.unitPath(b)
	if err != nil {
		return false, err
	}
	return fileExists(path), nil
}

func (s systemdUser) logs(b *Battery, n int, follow bool) error {
	args := []string{"--user", "--unit", s.unitName(b), "--no-pager", "--lines", strconv.Itoa(n)}
	if follow {
		args = append(args, "--follow")
	}
	return b.tool.stream(b.cfg.Stdout, "journalctl", args...)
}
