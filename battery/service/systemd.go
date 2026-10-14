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

// unitPath is the unit file, kept in the installed state directory.
// install registers it with `systemctl --user enable <path>`, which
// links it into the user manager's own configuration directory: the
// manager does not see the shell's XDG_CONFIG_HOME, so a file written
// to the directory the shell names may be one it never reads.
func (s systemdUser) unitPath(b *Battery) (string, error) {
	inst, err := b.layoutFor(false)
	if err != nil {
		return "", err
	}
	return filepath.Join(inst.dir, s.unitName(b)), nil
}

// dropInDir is where a user's own overrides for the unit live, in the
// conventional per-user configuration directory.
func (s systemdUser) dropInDir(b *Battery) (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" || !filepath.IsAbs(base) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("service: resolve home dir: %w", err)
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "systemd", "user", s.unitName(b)+".d"), nil
}

// systemdBooted reports whether systemd is the running init
// (sd_booted(3)); a variable so tests reach install's argv on a Mac.
var systemdBooted = func() bool { return fileExists("/run/systemd/system") }

// unit renders the definition. Output goes to the journal (stdout and
// stderr are captured by default), which `logs` reads back.
func (systemdUser) unit(b *Battery, args []string) []byte {
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
	args, err := b.daemonArgv(exe, port)
	if err != nil {
		return err
	}
	if err := ensurePrivateDir(filepath.Dir(path)); err != nil {
		return err
	}
	if err := fileperm.WriteOwnerOnly(path, s.unit(b, args)); err != nil {
		return fmt.Errorf("service: write %s: %w", path, err)
	}
	if err := b.run("systemctl", "--user", "daemon-reload"); err != nil {
		return err
	}
	// A path links the file into the manager's search path and
	// enables it in one step.
	if err := b.run("systemctl", "--user", "enable", path); err != nil {
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
	dropIn, err := s.dropInDir(b)
	if err != nil {
		return err
	}
	// disable removes the enable and link symlinks; --now stops the
	// unit and waits for it. A failure here is not "not installed": the
	// unit may still be running, so it is returned and nothing is
	// deleted.
	if fileExists(path) {
		if err := b.run("systemctl", "--user", "disable", "--now", s.unitName(b)); err != nil {
			return err
		}
	}
	if err := removeIn(filepath.Dir(path), filepath.Base(path)); err != nil {
		return fmt.Errorf("service: remove %s: %w", path, err)
	}
	// A drop-in directory left behind would re-apply to a later
	// install of the same id (kardianos/service #412).
	if err := os.RemoveAll(dropIn); err != nil {
		return fmt.Errorf("service: remove %s: %w", dropIn, err)
	}
	_, _ = b.tool.output("systemctl", "--user", "daemon-reload")
	return nil
}

// removeIn deletes name inside dir through an os.Root, so a symlinked
// component cannot point the delete outside dir. A missing dir or file
// is already removed.
func removeIn(dir, name string) error {
	root, err := os.OpenRoot(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer root.Close()
	if err := root.Remove(name); err != nil && !os.IsNotExist(err) {
		return err
	}
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
