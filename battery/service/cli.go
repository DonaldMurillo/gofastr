package service

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// startWait bounds how long install, start and restart wait for the
// daemon to answer on its control channel.
const startWait = 15 * time.Second

func (b *Battery) usage(w io.Writer) {
	n := b.cfg.Name
	fmt.Fprintf(w, `usage: %[1]s                       run in the foreground (development)
       %[1]s service <command>

commands:
  install [--port N]    copy this binary to a per-user location, register it
                        with the OS service manager (%[2]s), and start it
  uninstall [--purge]   stop and unregister; --purge also deletes the data
  start | stop | restart
  status                whether it is installed and running, and where
  logs [-n N] [-f]      the service log
  open [--dev]          sign in: open the app in your browser
  run [--port N] [--open]
                        run in the foreground with its own data and port
  rotate-secret [--dev] replace the secret, signing every browser out
`, n, b.mgr.kind())
}

func (b *Battery) dispatch(ctx context.Context, args []string, build Builder) error {
	if len(args) == 0 {
		b.usage(b.cfg.Stderr)
		return errors.New("service: missing command")
	}
	verb, rest := args[0], args[1:]
	fs := flag.NewFlagSet(b.cfg.Name+" service "+verb, flag.ContinueOnError)
	fs.SetOutput(b.cfg.Stderr)
	switch verb {
	case "install":
		port := fs.Int("port", b.cfg.Port, "loopback port")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		return b.install(*port)
	case "uninstall":
		purge := fs.Bool("purge", false, "also delete the data directory")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		return b.uninstall(*purge)
	case "start", "stop", "restart", "status":
		if err := fs.Parse(rest); err != nil {
			return err
		}
		return b.lifecycle(verb)
	case "logs":
		n := fs.Int("n", 100, "number of lines")
		follow := fs.Bool("f", false, "keep printing new lines")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		return b.mgr.logs(b, *n, *follow)
	case "open":
		dev := fs.Bool("dev", false, "sign in to the foreground dev run")
		print := fs.Bool("print", false, "print a one-shot URL (needs Config.InsecureTokenInURL)")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		l, err := b.layoutFor(*dev)
		if err != nil {
			return err
		}
		return b.openWith(l, *print)
	case "run":
		return b.runForeground(ctx, rest, build)
	case "daemon":
		port := fs.Int("port", b.cfg.Port, "loopback port")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		if err := validatePort("--port", *port, false); err != nil {
			return err
		}
		return b.serve(ctx, serveMode{port: *port}, build)
	case "rotate-secret":
		dev := fs.Bool("dev", false, "rotate the dev run's secret")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		return b.rotate(*dev)
	case "help", "-h", "--help":
		b.usage(b.cfg.Stdout)
		return nil
	}
	b.usage(b.cfg.Stderr)
	return fmt.Errorf("service: unknown command %q", verb)
}

func (b *Battery) runForeground(ctx context.Context, args []string, build Builder) error {
	fs := flag.NewFlagSet(b.cfg.Name+" service run", flag.ContinueOnError)
	fs.SetOutput(b.cfg.Stderr)
	port := fs.Int("port", b.cfg.DevPort, "loopback port (0 picks a free one)")
	open := fs.Bool("open", false, "open the browser once ready")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := validatePort("--port", *port, true); err != nil {
		return err
	}
	return b.serve(ctx, serveMode{dev: true, port: *port, openBrowser: *open}, build)
}

// runDevLoop is the child `gofastr dev` starts: `<app> --addr <addr>`.
// It is the foreground dev run on that address's port, gate and all:
// the browser the dev loop opens visits localhost:<port>, which the
// gate accepts, and a session survives every rebuild because the dev
// run's secret and port do not change. Sign in once with
// `<app> service open --dev`. The service host serves loopback only,
// so a host other than localhost or 127.0.0.1 is refused.
func (b *Battery) runDevLoop(ctx context.Context, args []string, build Builder) error {
	fs := flag.NewFlagSet(b.cfg.Name, flag.ContinueOnError)
	fs.SetOutput(b.cfg.Stderr)
	addr := fs.String("addr", "", "loopback listen address (set by gofastr dev)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	port, err := loopbackPort(*addr)
	if err != nil {
		return err
	}
	return b.serve(ctx, serveMode{dev: true, port: port}, build)
}

// loopbackPort returns the port of a loopback listen address: host
// empty, localhost, or 127.0.0.1.
func loopbackPort(addr string) (int, error) {
	host, p, err := net.SplitHostPort(addr)
	if err != nil {
		return 0, fmt.Errorf("service: --addr %q: %w", addr, err)
	}
	switch host {
	case "", "localhost", "127.0.0.1":
	default:
		return 0, fmt.Errorf("service: --addr %q: the service host serves 127.0.0.1 only", addr)
	}
	port, err := strconv.Atoi(p)
	if err != nil {
		return 0, fmt.Errorf("service: --addr %q: bad port", addr)
	}
	if err := validatePort("--addr", port, true); err != nil {
		return 0, err
	}
	return port, nil
}

// geteuid is os.Geteuid, a var so a test can play root.
var geteuid = os.Geteuid

// refuseRoot stops a per-user install from landing in root's account
// by way of sudo.
func refuseRoot() error {
	if currentOS != "windows" && geteuid() == 0 {
		return errors.New("service: refusing to install a per-user service as root; run this as the user who will use the app (system-wide install is not supported yet)")
	}
	return nil
}

func (b *Battery) install(port int) error {
	if err := validatePort("--port", port, false); err != nil {
		return err
	}
	if _, ok := b.mgr.(unsupported); ok {
		return b.mgr.install(b, "", port)
	}
	if err := refuseRoot(); err != nil {
		return err
	}
	inst, err := b.layoutFor(false)
	if err != nil {
		return err
	}
	if err := inst.ensure(); err != nil {
		return err
	}
	// A running copy holds the old binary (Windows refuses to replace a
	// running .exe) and the port.
	installed, err := b.mgr.installed(b)
	if err != nil {
		return err
	}
	if installed {
		_ = b.mgr.stop(b)
	}
	exe, err := b.copySelf()
	if err != nil {
		return err
	}
	if err := b.mgr.install(b, exe, port); err != nil {
		return err
	}
	fmt.Fprintf(b.cfg.Stdout, "installed %s with %s: %s\n", b.cfg.Name, b.mgr.kind(), exe)
	resp, err := b.waitReady(inst)
	if err != nil {
		return err
	}
	fmt.Fprintf(b.cfg.Stdout, "running at http://%s (pid %d)\nsign in with: %s service open\n", resp.Addr, resp.PID, b.cfg.Name)
	if currentOS == "linux" {
		fmt.Fprintf(b.cfg.Stdout, "to keep it running while you are logged out: loginctl enable-linger\n")
	}
	return nil
}

// copySelf copies the running executable to the managed location, so
// the registered program does not depend on where this one was run
// from (a Downloads folder, a `go run` temp dir). Write to a temp file
// in the target directory, then rename: the swap is atomic and a
// half-written binary is never registered.
func (b *Battery) copySelf() (string, error) {
	src, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("service: locate this executable: %w", err)
	}
	if r, err := filepath.EvalSymlinks(src); err == nil {
		src = r
	}
	dst, err := b.installedExe()
	if err != nil {
		return "", err
	}
	if err := ensurePrivateDir(filepath.Dir(dst)); err != nil {
		return "", err
	}
	if si, err := os.Stat(src); err == nil {
		if di, err := os.Stat(dst); err == nil && os.SameFile(si, di) {
			return dst, nil
		}
	}
	in, err := os.Open(src)
	if err != nil {
		return "", fmt.Errorf("service: read this executable: %w", err)
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(dst), "."+b.exeName()+"-*")
	if err != nil {
		return "", fmt.Errorf("service: copy executable: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		return "", fmt.Errorf("service: copy executable: %w", err)
	}
	if err := tmp.Chmod(0o700); err != nil {
		tmp.Close()
		return "", fmt.Errorf("service: copy executable: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("service: copy executable: %w", err)
	}
	if err := os.Rename(tmp.Name(), dst); err != nil {
		return "", fmt.Errorf("service: install executable: %w", err)
	}
	return dst, nil
}

func (b *Battery) uninstall(purge bool) error {
	if err := b.mgr.uninstall(b); err != nil {
		return err
	}
	dir, err := b.binDir()
	if err != nil {
		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("service: remove %s: %w", dir, err)
	}
	fmt.Fprintf(b.cfg.Stdout, "uninstalled %s\n", b.cfg.Name)
	if !purge {
		inst, err := b.layoutFor(false)
		if err == nil && fileExists(inst.dir) {
			fmt.Fprintf(b.cfg.Stdout, "data kept in %s (uninstall --purge deletes it)\n", inst.dir)
		}
		return nil
	}
	inst, err := b.layoutFor(false)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(inst.dir); err != nil {
		return fmt.Errorf("service: remove %s: %w", inst.dir, err)
	}
	fmt.Fprintf(b.cfg.Stdout, "deleted %s\n", inst.dir)
	return nil
}

func (b *Battery) lifecycle(verb string) error {
	inst, err := b.layoutFor(false)
	if err != nil {
		return err
	}
	installed, err := b.mgr.installed(b)
	if err != nil {
		return err
	}
	if verb == "status" {
		return b.status(inst, installed)
	}
	if !installed {
		return fmt.Errorf("service: %s is not installed; run `%s service install`", b.cfg.Name, b.cfg.Name)
	}
	switch verb {
	case "start":
		if err := b.mgr.start(b); err != nil {
			return err
		}
	case "stop":
		if err := b.mgr.stop(b); err != nil {
			return err
		}
		fmt.Fprintf(b.cfg.Stdout, "stopped %s\n", b.cfg.Name)
		return nil
	case "restart":
		if err := b.mgr.restart(b); err != nil {
			return err
		}
	}
	resp, err := b.waitReady(inst)
	if err != nil {
		return err
	}
	fmt.Fprintf(b.cfg.Stdout, "running at http://%s (pid %d)\n", resp.Addr, resp.PID)
	return nil
}

func (b *Battery) status(inst layout, installed bool) error {
	w := b.cfg.Stdout
	if installed {
		fmt.Fprintf(w, "installed:  yes (%s)\n", b.mgr.kind())
	} else {
		fmt.Fprintf(w, "installed:  no\n")
	}
	resp, err := callControl(inst.socketPath(), "status")
	switch {
	case errors.Is(err, errNotRunning):
		fmt.Fprintf(w, "running:    no\n")
	case err != nil:
		return err
	default:
		fmt.Fprintf(w, "running:    yes, pid %d since %s\n", resp.PID, resp.Started)
		fmt.Fprintf(w, "address:    http://%s\n", resp.Addr)
		fmt.Fprintf(w, "version:    %s\n", resp.Version)
	}
	fmt.Fprintf(w, "data:       %s\n", inst.dir)
	if dev, err := b.layoutFor(true); err == nil {
		if resp, err := callControl(dev.socketPath(), "status"); err == nil {
			fmt.Fprintf(w, "dev run:    pid %d at http://%s\n", resp.PID, resp.Addr)
		}
	}
	return nil
}

// waitReady polls the control channel until the daemon answers. A
// daemon that exits at once (port taken) never answers, and the error
// points at the log, where it said why.
func (b *Battery) waitReady(inst layout) (controlResponse, error) {
	deadline := time.Now().Add(startWait)
	for {
		resp, err := callControl(inst.socketPath(), "status")
		if err == nil {
			return resp, nil
		}
		if !errors.Is(err, errNotRunning) || time.Now().After(deadline) {
			return controlResponse{}, fmt.Errorf("service: %s did not come up within %s; see `%s service logs`", b.cfg.Name, startWait, b.cfg.Name)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// waitStopped polls the installed daemon's control channel until
// nothing answers.
func (b *Battery) waitStopped() error {
	inst, err := b.layoutFor(false)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(startWait)
	for {
		if _, err := callControl(inst.socketPath(), "status"); errors.Is(err, errNotRunning) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("service: %s did not stop within %s", b.cfg.Name, startWait)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func (b *Battery) rotate(dev bool) error {
	l, err := b.layoutFor(dev)
	if err != nil {
		return err
	}
	if err := l.ensure(); err != nil {
		return err
	}
	if err := rotateSecret(l.secretPath()); err != nil {
		return err
	}
	fmt.Fprintf(b.cfg.Stdout, "rotated the secret in %s\n", l.dir)
	if _, err := callControl(l.socketPath(), "status"); err != nil {
		return nil
	}
	if dev {
		fmt.Fprintf(b.cfg.Stdout, "restart the dev run to apply it\n")
		return nil
	}
	return b.lifecycle("restart")
}
