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
	"github.com/DonaldMurillo/gofastr/framework/isolation"
	"github.com/DonaldMurillo/gofastr/framework/localapp"
	"github.com/DonaldMurillo/gofastr/internal/procalive"
)

const (
	readyTimeout = 60 * time.Second
)

var stopTimeout = 10 * time.Second

type serveMode struct {
	dev         bool
	port        int
	openBrowser bool
}

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
	release, err := lockInstance(l.dir)
	if err == nil {
		defer release()
		var secret string
		if secret, err = loadOrMintSecret(l.secretPath()); err != nil {
			return err
		}
		var ctl net.Listener
		if ctl, err = listenControl(l.socketPath()); err == nil {
			defer ctl.Close()
			return b.serveLocked(ctx, mode, build, l, secret, ctl)
		}
	}
	if errors.Is(err, errAlreadyRunning) {
		return fmt.Errorf("service: %s is already running (state dir %s); `%s service status` shows where", b.cfg.Name, l.dir, b.cfg.Name)
	}
	return err
}

func (b *Battery) serveLocked(ctx context.Context, mode serveMode, build Builder, l layout, secret string, ctl net.Listener) error {

	g, err := newGate(b.cfg.Name+" service open"+devFlag(mode.dev), secret, b.cfg.SessionTTL, b.cfg.InsecureTokenInURL)
	if err != nil {
		return err
	}
	g.open = append([]string(nil), b.cfg.OpenPrefixes...)
	if g.user, err = localapp.LoadOrMint(l.dir); err != nil {
		return fmt.Errorf("service: %w", err)
	}
	b.mu.Lock()
	b.gate = g
	b.mu.Unlock()

	rt := Runtime{DataDir: l.dir, Secret: secret, Dev: mode.dev}
	app, err := build(rt)
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
	app.Config.DisableSignalHandling = true
	log := app.Logger()

	ready := make(chan string, 1)
	app.OnReady(func(addr string) {
		if _, p, err := net.SplitHostPort(addr); err == nil {
			if port, err := strconv.Atoi(p); err == nil {
				g.pinAt(browserAddr(addr), port, b.extraHosts(port))
			}
		}
		select {
		case ready <- addr:
		default:
		}
	})
	if !mode.dev {
		os.Setenv(isolation.EnvIsolation, "off")
	}

	app.SetLocalRunInfo(framework.LocalRunInfo{Service: b.cfg.ID, MCPCommand: rt.MCPCommand(), Dev: mode.dev})

	listen := net.JoinHostPort(b.bindHost(), strconv.Itoa(mode.port))
	startErr := make(chan error, 1)
	startDone := make(chan struct{})
	go func() {
		startErr <- app.Start(listen)
		close(startDone)
	}()

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
	if host, _, err := net.SplitHostPort(addr); err != nil || !b.boundHostOK(host) {
		b.drain(app, log)
		return fmt.Errorf("service: the app bound %q; the service host asked for %s (Config.BindAddress)", addr, b.bindHost())
	}

	stop := make(chan struct{}, 1)
	started := time.Now().UTC()
	cs := &controlServer{ln: ctl, log: log, handle: b.controlHandler(g, browserAddr(addr), mode.dev, started, stop),
		mcp: mcpSession(app.MCP, g.user)}
	go cs.serve()

	if len(b.cfg.AllowHosts) > 0 {
		log.Warn("service: extra Host values accepted (Config.AllowHosts)", "hosts", b.cfg.AllowHosts)
	}
	if b.cfg.BindAddress != "" && !isLoopbackIP(b.cfg.BindAddress) {
		log.Warn("service: listening beyond this machine's loopback; other machines and containers can reach the app (Config.BindAddress)", "addr", addr)
	}
	if len(g.open) > 0 {
		log.Warn("service: these paths answer without the browser sign-in; the app's own auth must protect them (Config.OpenPrefixes)", "prefixes", g.open)
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
	deadline := time.Now().Add(stopTimeout)
	cs.endMCP(time.Now().Add(mcpStopWait))
	b.drainUntil(app, log, deadline)
	cs.closeUntil(deadline)
	t := time.NewTimer(time.Until(deadline))
	defer t.Stop()
	select {
	case <-startDone:
	case <-t.C:
	}
	return runErr
}

func (b *Battery) drain(app *framework.App, log *slog.Logger) {
	b.drainUntil(app, log, time.Now().Add(stopTimeout))
}

func (b *Battery) drainUntil(app *framework.App, log *slog.Logger, deadline time.Time) {
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	if err := app.Shutdown(ctx); err != nil {
		log.Error("service: graceful shutdown incomplete", "error", err)
	}
}

func (b *Battery) controlHandler(g *gate, addr string, dev bool, started time.Time, stop chan<- struct{}) func(string) controlResponse {
	self := procalive.Identify(os.Getpid())
	return func(op string) controlResponse {
		switch op {
		case "status":
			return controlResponse{OK: true, PID: self.PID, ProcStart: self.Start, Addr: addr, Version: buildVersion(), Dev: dev,
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
			return controlResponse{OK: true, PID: self.PID, ProcStart: self.Start}
		}
		return controlResponse{Error: "unknown op"}
	}
}

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
