package framework

import (
	"context"
	"fmt"
	"github.com/DonaldMurillo/gofastr/internal/sdlisten"
	"net"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"sync"
	"syscall"
)

// httpHost is the built-in HTTP host behind Start and Serve.
type httpHost struct {
	app     *App
	addr    string
	network string
	ln      net.Listener
	srv     *http.Server // guarded by app.serverMu
}

func (h *httpHost) preflight() error {
	a := h.app
	if err := a.checkRequestWriteTimeouts(); err != nil {
		return err
	}
	if err := a.checkIdempotencyInFlightTTL(); err != nil {
		return err
	}
	h.network = "tcp"
	if h.ln == nil {
		passed, err := sdlisten.Inherited()
		if err != nil {
			return fmt.Errorf("listen and serve: %w", err)
		}
		if passed != nil {
			h.ln = &onceCloseListener{Listener: passed}
		}
	}
	if h.ln != nil {
		h.addr, h.network = h.ln.Addr().String(), h.ln.Addr().Network()
		return nil
	}
	return a.resolveListenAddr(&h.addr)
}

func (h *httpHost) prepare() {
	h.app.guardDevMCPBind(listenAddrFor(h.addr), h.network)
	h.app.guardDevLiveReloadBind(listenAddrFor(h.addr), h.network)
}

func (h *httpHost) Run(_ context.Context, s *Session) error {
	a := h.app
	name := a.Config.Name
	if name == "" {
		name = "gofastr"
	}

	os.Args[0] = "gofastr-" + name

	srv := &http.Server{
		Addr:              h.addr,
		Handler:           h.handler(s),
		ConnState:         a.trackConnState,
		ReadHeaderTimeout: defaultServerReadHeaderTimeout,
		ReadTimeout:       defaultServerReadTimeout,
		WriteTimeout:      defaultServerWriteTimeout,
		IdleTimeout:       defaultServerIdleTimeout,
	}
	srv.RegisterOnShutdown(a.closeNewConns)
	to := a.Config.HTTPServerTimeouts
	if to.ReadHeaderTimeout != nil {
		srv.ReadHeaderTimeout = *to.ReadHeaderTimeout
	}
	if to.ReadTimeout != nil {
		srv.ReadTimeout = *to.ReadTimeout
	}
	if to.WriteTimeout != nil {
		srv.WriteTimeout = *to.WriteTimeout
	}
	if to.IdleTimeout != nil {
		srv.IdleTimeout = *to.IdleTimeout
	}
	if a.Config.DisableRequestTimeout {
		if to.ReadTimeout == nil {
			srv.ReadTimeout = 0
		}
		if to.WriteTimeout == nil {
			srv.WriteTimeout = 0
		}
	}
	if budget := a.requestBudget(); budget > 0 && to.WriteTimeout == nil && srv.WriteTimeout <= budget {
		srv.WriteTimeout = budget + requestTimeoutWriteSlack
	}
	if p := a.Config.HTTPProtocols; p != nil {
		protos := *p
		srv.Protocols = &protos
	}
	tlsCfg, err := a.serverTLSConfig()
	if err != nil {
		return err
	}
	srv.TLSConfig = tlsCfg
	a.serverMu.Lock()
	if a.stopRequestedLocked() {
		a.serverMu.Unlock()
		return nil
	}
	a.server = srv
	h.srv = srv
	ln := h.ln
	a.serverMu.Unlock()
	if ln == nil {
		bound, err := net.Listen("tcp", listenAddrFor(h.addr))
		if err != nil {
			return fmt.Errorf("listen and serve: %w", err)
		}
		ln = bound
	}
	defer func() { _ = ln.Close() }()
	a.serverMu.Lock()
	network := ln.Addr().Network()
	if host, _, splitErr := net.SplitHostPort(ln.Addr().String()); splitErr == nil && tcpNetwork(network) {
		a.boundHost = host
	}
	a.boundInProcess = inProcessNetwork(network)
	a.serverMu.Unlock()

	if !a.Config.DisableSignalHandling {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sigQuit := make(chan struct{})
		sigDone := make(chan struct{})
		go func() {
			defer close(sigDone)
			defer signal.Stop(sigCh)
			select {
			case sig := <-sigCh:
				timeout := a.signalShutdownTimeout()
				a.Logger().Info("shutdown signal received; draining",
					"signal", sig.String(), "timeout", timeout.String())
				ctx, cancel := context.WithTimeout(context.Background(), timeout)
				defer cancel()
				if err := a.Shutdown(ctx); err != nil {
					a.Logger().Error("graceful shutdown incomplete", "error", err)
				}
			case <-sigQuit:
			}
		}()
		defer func() {
			close(sigQuit)
			<-sigDone
		}()
	}

	defer a.registerLocalRun(ln.Addr().String())()

	var setupURL string
	if s.setupInteractive {
		setupURL = a.setup.SetupURL(a.urlScheme(), ln.Addr().String())
	}
	a.printStartupBanner(ln.Addr().String(), ln.Addr().Network(), name, s.hasAPI, s.hasLLMMD, setupURL)
	for _, fn := range a.readyHooks {
		if a.stopRequestedNow() {
			break
		}
		if rerr := a.runReadyHookSafe(fn, ln.Addr().String()); rerr != nil {
			return rerr
		}
	}
	serve := srv.Serve
	if srv.TLSConfig != nil {
		serve = func(ln net.Listener) error { return srv.ServeTLS(ln, "", "") }
	}
	if err := serve(ln); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("listen and serve: %w", err)
	}
	return nil
}

func (h *httpHost) handler(s *Session) http.Handler {
	a := h.app
	realHandler := a.roleHandler()
	serveHandler := realHandler
	if s.setupInteractive {
		var swapOnce sync.Once
		swap := func() {
			swapOnce.Do(func() {
				a.handlerCell.Store(&servingHandler{h: realHandler})
				a.startDeferred()
			})
		}
		liveness, readiness := a.healthHandlers()
		a.handlerCell.Store(&servingHandler{h: a.setup.Handler(swap, liveness, readiness)})
		serveHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if cell := a.handlerCell.Load(); cell != nil {
				cell.h.ServeHTTP(w, r)
			} else {
				realHandler.ServeHTTP(w, r)
			}
		})
	}
	a.serverMu.Lock()
	a.serverWrapped = true
	wrappers := slices.Clone(a.serverWrappers)
	a.serverMu.Unlock()
	for i := len(wrappers) - 1; i >= 0; i-- {
		serveHandler = wrappers[i](serveHandler)
	}
	serveHandler = a.serveLocale(serveHandler)
	return a.endStreamsOnShutdown(serveHandler)
}

func (h *httpHost) Stop(ctx context.Context) error {
	a := h.app
	a.serverMu.Lock()
	srv, ln := h.srv, h.ln
	a.serverMu.Unlock()
	if srv == nil {
		if ln != nil {
			_ = ln.Close()
		}
		return nil
	}
	a.streams.endAll(ctx)
	if err := srv.Shutdown(ctx); err != nil {
		_ = srv.Close()
		return fmt.Errorf("draining http server: %w", err)
	}
	return nil
}
