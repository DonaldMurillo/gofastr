package framework

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"sync"
	"syscall"
)

// httpHost is the built-in HTTP host behind Start (addr, bound here) and
// Serve (a caller's listener). It owns the server, the bind, the signal
// handler, the startup banner and OnReady; the lifecycle engine has run
// every boot phase before Run is called.
type httpHost struct {
	app     *App
	addr    string       // Start's address; preflight applies isolation
	network string       // set by preflight
	ln      net.Listener // Serve's listener (closes once); nil for Start
	srv     *http.Server // adopted by Run; guarded by app.serverMu
}

// preflight runs the HTTP-only checks that refuse a run before boot.
func (h *httpHost) preflight() error {
	a := h.app
	if err := a.checkRequestWriteTimeouts(); err != nil {
		return err
	}
	h.network = "tcp"
	if h.ln != nil {
		// A caller-supplied listener is served as given: there is no
		// address to remap, and the guards below read its own address
		// and network.
		h.addr, h.network = h.ln.Addr().String(), h.ln.Addr().Network()
		return nil
	}
	return a.resolveListenAddr(&h.addr)
}

// prepare withdraws the dev surfaces the bind would expose. They are
// registered during InitPlugins, so this runs before it.
func (h *httpHost) prepare() {
	h.app.guardDevMCPBind(listenAddrFor(h.addr), h.network)
	h.app.guardDevLiveReloadBind(listenAddrFor(h.addr), h.network)
}

// Run builds the server, binds, fires OnReady and serves until Shutdown.
func (h *httpHost) Run(_ context.Context, s *Session) error {
	a := h.app
	name := a.Config.Name
	if name == "" {
		name = "gofastr"
	}

	// Set process title so it shows in ps / Activity Monitor
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
	// net/http runs this right after Shutdown closes the listener, so it
	// also closes a connection accepted while Shutdown ended the streams.
	// The server is new on every Start, so the hook is registered once.
	srv.RegisterOnShutdown(a.closeNewConns)
	// Apply host overrides (AppConfig.HTTPServerTimeouts / WithHTTPServerTimeouts).
	// Each field is *time.Duration: nil keeps the default above; an explicit
	// pointer, including one to 0, wins. net/http treats a zero duration as
	// "no timeout", so a pointer to 0 disables that one deadline rather than
	// falling back to the default, which a plain time.Duration field could not
	// express.
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
		// The documented opt-out for SSE and long uploads drops the Timeout
		// middleware from the chain and zeroes the server-level read/write
		// deadlines to match (header and idle stay, they bound the
		// connection, not the request body or the stream). It zeroes only the
		// fields the host left unset: an explicit HTTPServerTimeouts value is
		// authoritative, so the fine-grained knob can keep a custom deadline
		// while still dropping the middleware.
		if to.ReadTimeout == nil {
			srv.ReadTimeout = 0
		}
		if to.WriteTimeout == nil {
			srv.WriteTimeout = 0
		}
	}
	if budget := a.requestBudget(); budget > 0 && to.WriteTimeout == nil && srv.WriteTimeout <= budget {
		// A RequestTimeout raised past the default write deadline: the
		// handler it was raised for would finish inside its budget and
		// lose its response at the deadline. An explicit WriteTimeout in
		// the same position is refused by checkRequestWriteTimeouts.
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
	// A Shutdown may have begun after boot ended. It found no server to
	// stop; adopting one NOW would serve forever with nobody left to stop
	// it. Shutdown records its stop under the lock checked here, before it
	// calls Stop, so the window is closed, not narrowed, and nil keeps the
	// graceful-shutdown contract: Start reports no error when told to stop.
	a.serverMu.Lock()
	if a.stopRequestedLocked() {
		a.serverMu.Unlock()
		return nil
	}
	a.server = srv
	h.srv = srv
	ln := h.ln
	a.serverMu.Unlock()
	// Bind first, then Serve, split from ListenAndServe so OnReady hooks
	// fire only after the port is actually held. http.ListenAndServe
	// defaults an empty Addr to ":http"; net.Listen needs that explicit.
	if ln == nil {
		bound, err := net.Listen("tcp", listenAddrFor(h.addr))
		if err != nil {
			// Bind failure (port in use is the common case): the engine
			// drains like every earlier start phase does, otherwise the
			// batteries/cron/queue and OnStart workers spawned during boot
			// leak past Start returning.
			return fmt.Errorf("listen and serve: %w", err)
		}
		ln = bound
	}
	// Serve closes the listener on Shutdown; this extra Close only matters
	// on the Shutdown-raced path where Serve returns without adopting ln.
	defer func() { _ = ln.Close() }()
	a.serverMu.Lock()
	// Only a TCP address is a host:port. A unix socket's address is its
	// path, and a path like "127.0.0.1:8080" must not read as loopback.
	network := ln.Addr().Network()
	if host, _, splitErr := net.SplitHostPort(ln.Addr().String()); splitErr == nil && tcpNetwork(network) {
		a.boundHost = host
	}
	a.boundInProcess = inProcessNetwork(network)
	a.serverMu.Unlock()

	// Graceful shutdown by default: docker stop and kubectl rollouts send
	// SIGTERM, and without a handler the runtime kills the process
	// mid-request, no drain, no battery stop, no OnStop hooks. The
	// deferred join keeps Start from returning while a signal-triggered
	// drain is still running (Serve returns ErrServerClosed as soon as
	// Shutdown begins). Opt out via DisableSignalHandling when the host
	// process owns signals and calls Shutdown/RunWithSignals itself;
	// concurrent Shutdown calls are safe. It is idempotent.
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

	// The local run record (framework/localrun) exists while Serve
	// does: written once the port is held, removed as Start returns.
	defer a.registerLocalRun(ln.Addr().String())()

	var setupURL string
	if s.setupInteractive {
		setupURL = a.setup.SetupURL(a.urlScheme(), ln.Addr().String())
	}
	a.printStartupBanner(ln.Addr().String(), ln.Addr().Network(), name, s.hasAPI, s.hasLLMMD, setupURL)
	for _, fn := range a.readyHooks {
		// No ready hook starts once a Shutdown has begun; Serve below then
		// returns as soon as Shutdown stops the server.
		if a.stopRequestedNow() {
			break
		}
		if rerr := a.runReadyHookSafe(fn, ln.Addr().String()); rerr != nil {
			return rerr
		}
	}
	serve := srv.Serve
	if srv.TLSConfig != nil {
		// ServeTLS adds h2 to the offered protocols and wraps ln; the
		// certificate comes from TLSConfig, so no files are named here.
		serve = func(ln net.Listener) error { return srv.ServeTLS(ln, "", "") }
	}
	if err := serve(ln); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("listen and serve: %w", err)
	}
	return nil
}

// handler composes what the server serves: the role's handler (the full
// router for all/serve, the health mux for worker), the interactive
// setup wizard in front of it until setup completes, the WrapServer
// wrappers, locale routing, and the stream end Shutdown triggers.
func (h *httpHost) handler(s *Session) http.Handler {
	a := h.app
	realHandler := a.roleHandler()
	serveHandler := realHandler
	if s.setupInteractive {
		// The server delegates to whatever handlerCell currently points
		// at, initially the setup wizard, swapped to realHandler by the
		// swap callback on completion.
		var swapOnce sync.Once
		swap := func() {
			swapOnce.Do(func() {
				// Serve the real router first: setup IS complete (the
				// runner only swaps after its Complete predicate flips),
				// so the app must never keep answering 503 from here on.
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
	// Latch WrapServer under its lock, then apply the wrappers outside
	// it: they are host code, and a later WrapServer could not take
	// effect, so it panics.
	a.serverMu.Lock()
	a.serverWrapped = true
	wrappers := slices.Clone(a.serverWrappers)
	a.serverMu.Unlock()
	for i := len(wrappers) - 1; i >= 0; i-- {
		serveHandler = wrappers[i](serveHandler)
	}
	serveHandler = a.serveLocale(serveHandler)
	// Outermost, so a stream's context is the one every layer below sees.
	return a.endStreamsOnShutdown(serveHandler)
}

// Stop drains the server, or, before one was adopted, closes the
// caller's listener: Run adopts no server once Shutdown has begun.
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
	// A connection that never sent a request would hold the drain for
	// five seconds. srv.Shutdown closes those through the closeNewConns
	// hook Run registered, once its listener is closed: the stream end
	// below can take two seconds, and a connection accepted during it
	// would be missed by a close run here.
	//
	// An event stream never goes idle and a WebSocket is outside the
	// drain: end each one now rather than wait out the deadline on it or
	// leave it serving past Shutdown (shutdown_streams.go).
	a.streams.endAll(ctx)
	if err := srv.Shutdown(ctx); err != nil {
		// Bounded drain: the deadline expired with connections still open
		// (an SSE stream never goes idle, so Server.Shutdown alone would
		// leave it, and the process, hanging). Force-close the stragglers
		// so shutdown completes.
		_ = srv.Close()
		return fmt.Errorf("draining http server: %w", err)
	}
	return nil
}
