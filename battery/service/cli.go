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
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/procalive"
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
  mcp [--dev] [--state-dir DIR]
                        serve the app's MCP over stdio, for an agent's
                        MCP config (the daemon must be running)
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
	case "mcp":
		dev := fs.Bool("dev", false, "connect to the foreground dev run")
		stateDir := fs.String("state-dir", "", "the daemon's state directory (absolute), as its run record names it")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		return b.mcpBridge(*dev, *stateDir, b.cfg.Stdin, b.cfg.Stdout)
	case "run":
		return b.runForeground(ctx, rest, build)
	case "daemon":
		port := fs.Int("port", b.cfg.Port, "loopback port")
		base := fs.String("base-dir", "", "state base directory (set by install)")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		if err := validatePort("--port", *port, false); err != nil {
			return err
		}
		if *base != "" {
			if !filepath.IsAbs(*base) {
				return fmt.Errorf("service: --base-dir must be absolute, got %q", *base)
			}
			b.baseOverride = *base
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
// It is the foreground dev run on that address's port, gate and all.
// Sign in once with `<app> service open --dev`; the session survives
// every rebuild because the dev run's secret and port do not change.
// The localhost:<port> address `gofastr dev` prints redirects to
// 127.0.0.1:<port>, where the session cookie lives. The service host
// serves loopback only, so a host other than localhost or 127.0.0.1
// is refused.
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
	// Refuse here, before anything is registered, rather than leave a
	// daemon that crash-loops on the same limit.
	if err := checkSocketPath(inst.socketPath()); err != nil {
		return err
	}
	if err := inst.ensure(); err != nil {
		return err
	}
	// A running copy holds the old binary (Windows refuses to replace a
	// running .exe) and the port; stop waits for it to exit.
	installed, err := b.mgr.installed(b)
	if err != nil {
		return err
	}
	if installed {
		if err := b.mgr.stop(b); err != nil {
			return fmt.Errorf("service: stop the installed copy before replacing it: %w", err)
		}
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
	daemon, err := b.runningDaemon()
	starting := errors.Is(err, errStarting)
	if err != nil && !starting {
		return err
	}
	if err := b.mgr.uninstall(b); err != nil {
		return err
	}
	// Whatever the manager reported, nothing is deleted while the
	// daemon is still alive: it holds the database and the binary. A
	// starting daemon named no pid, so the wait is on its control
	// channel going away.
	if daemon.PID > 0 || starting {
		if err := b.waitExit(daemon); err != nil {
			return fmt.Errorf("%w; nothing was deleted", err)
		}
	}
	dir, err := b.binDir()
	if err != nil {
		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		// Windows will not delete the executable a process is running
		// from, so the installed copy uninstalling itself cannot remove
		// its own folder. Say so and carry on to --purge.
		if !runningFrom(dir) {
			return fmt.Errorf("service: remove %s: %w", dir, err)
		}
		fmt.Fprintf(b.cfg.Stdout, "could not remove %s while running from it; delete it after this command exits\n", dir)
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
// points at the log, where it said why. A starting daemon has bound its
// socket but answers only once the app is ready, so a probe that dials
// and then times out waiting for the reply is starting, not failed.
func (b *Battery) waitReady(inst layout) (controlResponse, error) {
	deadline := time.Now().Add(startWait)
	for {
		resp, err := callControl(inst.socketPath(), "status")
		if err == nil {
			return resp, nil
		}
		starting := errors.Is(err, errNotRunning) || errors.Is(err, os.ErrDeadlineExceeded)
		if !starting || time.Now().After(deadline) {
			return controlResponse{}, fmt.Errorf("service: %s did not come up within %s; see `%s service logs`", b.cfg.Name, startWait, b.cfg.Name)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// errStarting reports a daemon whose control socket accepts but does
// not answer: it binds the socket before its builder runs and serves
// it only once the app is ready. A builder that never returns looks
// the same. There is a process, but no reply names its pid.
var errStarting = errors.New("starting")

// runningDaemon is the installed daemon's process, pid 0 when nothing
// answers on its control channel, as the daemon reports it. A daemon
// that dials but stays silent gets up to startingWait to answer; past
// that the result is pid 0 with errStarting, and the caller still has a
// process to stop.
func (b *Battery) runningDaemon() (procalive.Identity, error) {
	inst, err := b.layoutFor(false)
	if err != nil {
		return procalive.Identity{}, err
	}
	resp, err := callControl(inst.socketPath(), "status")
	for deadline := time.Now().Add(startingWait); errors.Is(err, os.ErrDeadlineExceeded) && time.Now().Before(deadline); {
		resp, err = callControl(inst.socketPath(), "status")
	}
	switch {
	case errors.Is(err, errNotRunning):
		return procalive.Identity{}, nil
	case errors.Is(err, os.ErrDeadlineExceeded):
		return procalive.Identity{}, errStarting
	case err != nil:
		return procalive.Identity{}, err
	}
	return resp.identity(), nil
}

// identity is the daemon process a reply names: its pid with the start
// value the daemon read itself. A daemon built before it reported one
// sends none, and the pid is sampled now instead; after a stop that
// sample may already be a newer process's.
func (r controlResponse) identity() procalive.Identity {
	if r.ProcStart != 0 {
		return procalive.Identity{PID: r.PID, Start: r.ProcStart}
	}
	return procalive.Identify(r.PID)
}

// stopWait bounds how long a stop waits for the process to exit: the
// daemon's own drain bound plus slack for the exit itself.
var stopWait = stopTimeout + 10*time.Second

// startingWait is how long stop keeps asking a daemon that dials but
// does not answer, the silence of one still building its app, before
// treating it as wedged.
var startingWait = startWait

// waitExit waits until the installed daemon's control channel is gone
// and the daemon process has exited. The daemon keeps the channel open
// until its drain finishes, but the channel closing is not the process
// exiting: until it exits, Windows holds its executable locked and a
// service manager still counts the job as running, so a start issued
// then is dropped. A pid the kernel has handed to a newer process is
// not the daemon (procalive.Identity), so a recycled pid does not hold
// stop for the full wait or make uninstall refuse.
func (b *Battery) waitExit(daemon procalive.Identity) error {
	inst, err := b.layoutFor(false)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(stopWait)
	for {
		_, err := callControl(inst.socketPath(), "status")
		if errors.Is(err, errNotRunning) && (daemon.PID <= 0 || !daemon.Alive()) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("service: %s (pid %d) did not exit within %s", b.cfg.Name, daemon.PID, stopWait)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// runningFrom reports whether this process's executable is inside dir.
func runningFrom(dir string) bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	if r, err := filepath.EvalSymlinks(dir); err == nil {
		dir = r
	}
	rel, err := filepath.Rel(dir, exe)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// daemonArgv is the argv install registers for exe on port, with the
// state base this CLI resolved.
func (b *Battery) daemonArgv(exe string, port int) ([]string, error) {
	base, err := b.base()
	if err != nil {
		return nil, err
	}
	return daemonArgs(exe, port, base), nil
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
	if _, err := callControl(l.socketPath(), "status"); errors.Is(err, errNotRunning) {
		return nil
	} else if err != nil {
		// Something holds the socket but did not answer: it may still
		// sign sessions with the old secret.
		return fmt.Errorf("service: the secret was rotated, but the running %s did not answer (%w); restart it to apply the new secret", b.cfg.Name, err)
	}
	if dev {
		fmt.Fprintf(b.cfg.Stdout, "restart the dev run to apply it\n")
		return nil
	}
	return b.lifecycle("restart")
}
