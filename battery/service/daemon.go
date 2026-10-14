package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"runtime/debug"
	"strconv"
	"syscall"
	"time"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/internal/localuser"
)

const (
	// readyTimeout bounds the wait for the App's listener: migrations
	// and seeds run before the bind.
	readyTimeout = 60 * time.Second
	// stopTimeout bounds the graceful drain. Windows gives a logoff
	// little time, so this stays well under systemd's TimeoutStopSec.
	stopTimeout = 10 * time.Second
)

// serveMode is one way of running the App: the installed daemon or
// the foreground dev run.
type serveMode struct {
	dev         bool
	port        int
	openBrowser bool
}

// serve builds the App, binds it on loopback, opens the control
// channel, and blocks until a signal, a control "stop", or ctx ends.
// A graceful stop returns nil, so the process exits 0 and neither
// launchd (KeepAlive SuccessfulExit=false) nor systemd
// (Restart=on-failure) restarts it.
func (b *Battery) serve(ctx context.Context, mode serveMode, build Builder) error {
	l, err := b.layoutFor(mode.dev)
	if err != nil {
		return err
	}
	if err := l.ensure(); err != nil {
		return err
	}
	if !mode.dev {
		if err := redirectDaemonOutput(l.logPath()); err != nil {
			return err
		}
	}
	secret, err := loadOrMintSecret(l.secretPath())
	if err != nil {
		return err
	}
	// Claim the control socket before building the App: a second
	// instance fails here, before it opens the database or runs a
	// migration against a file the first one is using.
	ctl, err := listenControl(l.socketPath())
	if errors.Is(err, errAlreadyRunning) {
		return fmt.Errorf("service: %s is already running (state dir %s); `%s service status` shows where", b.cfg.Name, l.dir, b.cfg.Name)
	}
	if err != nil {
		return err
	}
	defer ctl.Close()

	g, err := newGate(b.cfg.Name+" service open"+devFlag(mode.dev), secret, b.cfg.SessionTTL, b.cfg.InsecureTokenInURL)
	if err != nil {
		return err
	}
	// The signed-in browser is the OS user the service runs as: the
	// gate puts that local identity on every request it lets through
	// (unless battery/auth owns identity; see Init).
	if g.user, err = localuser.LoadOrMint(l.dir); err != nil {
		return fmt.Errorf("service: %w", err)
	}
	b.mu.Lock()
	b.gate = g
	b.mu.Unlock()

	app, err := build(Runtime{DataDir: l.dir, Secret: secret, Dev: mode.dev})
	if err != nil {
		return fmt.Errorf("service: build app: %w", err)
	}
	if app == nil {
		return errors.New("service: the builder returned a nil *framework.App")
	}
	if got, err := framework.GetAs[*Battery](app.Batteries, b.Name()); err != nil {
		app.RegisterBattery(b)
	} else if got != b {
		return errors.New("service: the App has a different service battery registered; register this one, or none and let Run register it")
	}
	// The battery owns signals: it must send STOPPING=1 before the
	// drain and close the control channel with it.
	app.Config.DisableSignalHandling = true
	log := app.Logger()

	ready := make(chan string, 1)
	app.OnReady(func(addr string) {
		// Pin before Serve accepts the first connection: until then the
		// gate refuses everything.
		if _, p, err := net.SplitHostPort(addr); err == nil {
			if port, err := strconv.Atoi(p); err == nil {
				g.pin(port, b.cfg.AllowHosts)
			}
		}
		select {
		case ready <- addr:
		default:
		}
	})
	if !mode.dev {
		// The installed port is the contract (bookmarks, the
		// definition's argv); worktree isolation must not remap it.
		os.Setenv("GOFASTR_ISOLATION", "off")
	}

	listen := "127.0.0.1:" + strconv.Itoa(mode.port)
	startErr := make(chan error, 1)
	go func() { startErr <- app.Start(listen) }()

	var addr string
	select {
	case addr = <-ready:
	case err := <-startErr:
		if err == nil {
			err = errors.New("server returned before it was ready")
		}
		return fmt.Errorf("service: start %s on %s: %w", b.cfg.Name, listen, err)
	case <-time.After(readyTimeout):
		b.drain(app, log)
		return fmt.Errorf("service: %s did not report ready within %s", b.cfg.Name, readyTimeout)
	}
	if host, _, err := net.SplitHostPort(addr); err != nil || host != "127.0.0.1" {
		b.drain(app, log)
		return fmt.Errorf("service: the app bound %q; the service host requires 127.0.0.1", addr)
	}

	stop := make(chan struct{}, 1)
	started := time.Now().UTC()
	cs := &controlServer{ln: ctl, log: log, handle: b.controlHandler(g, addr, mode.dev, started, stop)}
	go cs.serve()

	if len(b.cfg.AllowHosts) > 0 {
		log.Warn("service: extra Host values accepted (Config.AllowHosts)", "hosts", b.cfg.AllowHosts)
	}
	if b.cfg.InsecureTokenInURL {
		log.Warn("service: sign-in tokens may be printed as URLs (Config.InsecureTokenInURL); they pass through argv and browser history")
	}
	_ = sdNotify("READY=1\nSTATUS=serving on " + addr)
	log.Info("service: ready", "addr", "http://"+addr, "dev", mode.dev,
		"sign_in", "`"+b.cfg.Name+" service open"+devFlag(mode.dev)+"`")
	if mode.openBrowser {
		go func() {
			if err := b.openWith(l, false); err != nil {
				log.Warn("service: could not open the browser", "error", err.Error())
			}
		}()
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sig)

	var runErr error
	select {
	case s := <-sig:
		log.Info("service: stopping", "signal", s.String())
	case <-stop:
		log.Info("service: stopping", "requested_by", "control channel")
	case <-ctx.Done():
		log.Info("service: stopping", "reason", ctx.Err().Error())
	case err := <-startErr:
		runErr = fmt.Errorf("service: server stopped: %w", err)
	}
	_ = sdNotify("STOPPING=1")
	cs.Close()
	if err := b.drain(app, log); err != nil && runErr == nil {
		runErr = err
	}
	select {
	case <-startErr:
	case <-time.After(stopTimeout):
	}
	return runErr
}

// drain shuts the App down with a bounded context.
func (b *Battery) drain(app *framework.App, log *slog.Logger) error {
	ctx, cancel := context.WithTimeout(context.Background(), stopTimeout)
	defer cancel()
	if err := app.Shutdown(ctx); err != nil {
		log.Error("service: graceful shutdown incomplete", "error", err)
		return fmt.Errorf("service: shutdown: %w", err)
	}
	return nil
}

// controlHandler answers the control channel's ops.
func (b *Battery) controlHandler(g *gate, addr string, dev bool, started time.Time, stop chan<- struct{}) func(string) controlResponse {
	return func(op string) controlResponse {
		switch op {
		case "status":
			return controlResponse{OK: true, PID: os.Getpid(), Addr: addr, Version: buildVersion(), Dev: dev,
				Started: started.Format(time.RFC3339)}
		case "token":
			tok, err := g.mintToken()
			if err != nil {
				return controlResponse{Error: err.Error()}
			}
			return controlResponse{OK: true, Addr: addr, Token: tok}
		case "stop":
			select {
			case stop <- struct{}{}:
			default:
			}
			return controlResponse{OK: true}
		}
		return controlResponse{Error: "unknown op"}
	}
}

// buildVersion is the main module's version from the build info, or
// "(devel)".
func buildVersion() string {
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" {
		return bi.Main.Version
	}
	return "(devel)"
}

func devFlag(dev bool) string {
	if dev {
		return " --dev"
	}
	return ""
}

// sdNotify sends one systemd notification datagram to $NOTIFY_SOCKET.
// The protocol is a stable, dependency-free contract (sd_notify(3));
// outside systemd the variable is unset and this does nothing. A
// leading "@" names an abstract socket, which Go's net package
// understands as written.
func sdNotify(state string) error {
	path := os.Getenv("NOTIFY_SOCKET")
	if path == "" || (path[0] != '/' && path[0] != '@') {
		return nil
	}
	c, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: path, Net: "unixgram"})
	if err != nil {
		return err
	}
	defer c.Close()
	_, err = c.Write([]byte(state))
	return err
}
