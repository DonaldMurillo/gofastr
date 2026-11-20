package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/internal/fileperm"
)

type systemdUser struct{}

func (systemdUser) kind() string { return "systemd --user" }

func (systemdUser) unitName(b *Battery) string { return b.cfg.ID + ".service" }

func (s systemdUser) unitPath(b *Battery) (string, error) {
	inst, err := b.layoutFor(false)
	if err != nil {
		return "", err
	}
	return filepath.Join(inst.dir, s.unitName(b)), nil
}

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

var systemdBooted = func() bool { return fileExists("/run/systemd/system") }

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
	//gofastr:allow(rootwrite) the user's own systemd unit directory; the service ID is developer configuration
	if err := ensurePrivateDir(filepath.Dir(path)); err != nil {
		return err
	}
	if err := fileperm.WriteOwnerOnly(path, s.unit(b, args)); err != nil {
		return fmt.Errorf("service: write %s: %w", path, err)
	}
	if err := b.run("systemctl", "--user", "daemon-reload"); err != nil {
		return err
	}
	if err := b.run("systemctl", "--user", "enable", path); err != nil {
		return err
	}
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
	if fileExists(path) { //gofastr:allow(rootread) the user's own systemd unit path, developer configuration like the rootwrite allowance above
		if err := b.run("systemctl", "--user", "disable", "--now", s.unitName(b)); err != nil {
			return err
		}
	}
	if err := removeIn(filepath.Dir(path), filepath.Base(path)); err != nil {
		return fmt.Errorf("service: remove %s: %w", path, err)
	}
	if err := os.RemoveAll(dropIn); err != nil {
		return fmt.Errorf("service: remove %s: %w", dropIn, err)
	}
	_, _ = b.tool.output("systemctl", "--user", "daemon-reload")
	return nil
}

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
	return fileExists(path), nil //gofastr:allow(rootread) the user's own systemd unit path, developer configuration like the rootwrite allowance above
}

func (s systemdUser) logs(b *Battery, n int, follow bool) error {
	args := []string{"--user", "--unit", s.unitName(b), "--no-pager", "--lines", strconv.Itoa(n)}
	if follow {
		args = append(args, "--follow")
	}
	return b.tool.stream(b.cfg.Stdout, "journalctl", args...)
}
