package log

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework"
)

// Config configures the log plugin.
//
// The zero value Config{} is the canonical setup: a per-app file sink
// resolved under the OS state dir, access logging on, panic recovery on,
// lifecycle events on, App.Logger swapped to the plugin's logger so
// framework middleware (Logging, etc.) writes through the same sinks.
type Config struct {
	// Level is the minimum slog level emitted. Zero value = LevelInfo.
	Level slog.Level

	// Sinks receive every log entry. If empty, a DefaultFileSink is used.
	//
	// Order matters on shutdown: sinks close in the order they appear
	// here. Put fast/local sinks (file) LAST so they outlive slow
	// remote sinks (webhook) — operators grep'ing the file during an
	// in-flight shutdown will see fresh entries longer that way.
	Sinks []Sink

	// DisableLifecycleEvents skips the "app.start" / "app.stop" entries.
	DisableLifecycleEvents bool

	// AddSource adds source file:line to every entry. Default false.
	AddSource bool

	// TrustForwardedFor opts in to reading the client IP from the
	// `X-Forwarded-For` / `X-Real-IP` headers in the access log's
	// `remote` field. Off by default because those headers are
	// trivially spoofable by direct clients; turn on only when the app
	// sits behind a trusted proxy that overwrites them.
	//
	// Even when off the raw value is still emitted as `forwarded_for`
	// so operators can correlate without trusting it.
	TrustForwardedFor bool

	// EnableMCP installs a RingSink + MCP tools (log_recent, log_filter,
	// log_metrics, log_set_level) so a connected agent can debug the
	// running app via the plugin's logger surface. Off by default.
	// Production apps that expose MCP to untrusted callers should weigh
	// the disclosure surface before enabling.
	EnableMCP bool

	// MCPRingSize is the ring buffer capacity used when EnableMCP is
	// true. Zero defaults to 1000 entries.
	MCPRingSize int
}

// Plugin is the log plugin. Implements framework.Plugin.
type Plugin struct {
	cfg     Config
	logger  *slog.Logger
	handler *fanoutHandler

	// ring is the in-memory query backing store when EnableMCP is set.
	// nil otherwise. Held on the plugin so MCP tool handlers (closures
	// over *Plugin) can read it.
	ring *RingSink

	// level holds the current minimum level — a pointer to a slog.LevelVar
	// so log_set_level can mutate it without re-wiring the handler.
	level *slog.LevelVar

	// resolvedFilePath is the path of the DefaultFileSink when one was
	// auto-installed. Populated during Init so the log_filter tool can
	// tail the file for historical entries beyond the ring window.
	resolvedFilePath string
}

// New constructs an unregistered plugin. Call App.RegisterPlugin to wire.
func New(cfg Config) *Plugin {
	if cfg.Level == 0 {
		cfg.Level = slog.LevelInfo
	}
	return &Plugin{cfg: cfg}
}

// Name implements framework.Plugin.
func (p *Plugin) Name() string { return "log" }

// Logger returns the *slog.Logger that fans out to every configured sink.
// Useful for app code that wants explicit access without relying on
// slog.Default.
func (p *Plugin) Logger() *slog.Logger { return p.logger }

// Metrics is a point-in-time snapshot of counters surfaced by the log
// plugin. Operators can scrape these via /metrics, /readyz, or any
// other surface — the values are atomic loads, cheap and lock-free.
type Metrics struct {
	// PostStopDrops counts log entries discarded because they arrived
	// after sinks were closed (worker goroutines or stop hooks logging
	// during shutdown).
	PostStopDrops uint64
	// SinkWriteFailures counts log entries dropped because a sink's
	// Write returned a non-ErrSinkClosed error (disk full, network).
	SinkWriteFailures uint64
	// WebhookDropped is the sum of entries dropped from every webhook
	// sink's bounded queue (drop-oldest under backpressure).
	WebhookDropped uint64
	// WebhookGaveUp is the sum of batches given up by every webhook
	// sink after exhausting MaxRetries.
	WebhookGaveUp uint64
}

// Metrics returns a point-in-time snapshot of the plugin's counters.
// Safe to call at any time; values are atomic loads.
func (p *Plugin) Metrics() Metrics {
	m := Metrics{
		PostStopDrops:     p.handler.metrics.closedDrops.Load(),
		SinkWriteFailures: p.handler.metrics.failedSinks.Load(),
	}
	for _, s := range p.handler.sinks {
		if ws, ok := s.(*webhookSink); ok {
			m.WebhookDropped += ws.Dropped()
			m.WebhookGaveUp += ws.GaveUp()
		}
	}
	return m
}

// Init implements framework.Plugin. Builds the fan-out handler, swaps
// the App's logger so framework middleware writes through it, attaches
// panic recovery + access log middleware, and registers lifecycle hooks
// for app.start / app.stop entries.
//
// Router late-binding means the panic-recovery and access-log middleware
// installed here wrap routes registered before this plugin was added,
// not just routes added later — there is no ordering footgun.
func (p *Plugin) Init(app *framework.App) error {
	cfg := p.cfg

	// Resolve sinks.
	sinks := cfg.Sinks
	if len(sinks) == 0 {
		dir, err := stateDir(app.Config.Name)
		if err != nil {
			return fmt.Errorf("log: default sink: %w", err)
		}
		path := filepath.Join(dir, "server.log")
		s, err := FileSink(path, FileOpts{})
		if err != nil {
			return fmt.Errorf("log: default sink: %w", err)
		}
		sinks = []Sink{s}
		p.resolvedFilePath = path
	}

	// If MCP is enabled, install an in-memory ring sink for fast queries.
	// Inserted at the FRONT of the sink list so the query path doesn't
	// depend on the file/webhook sinks succeeding.
	if cfg.EnableMCP {
		p.ring = NewRingSink(cfg.MCPRingSize)
		sinks = append([]Sink{p.ring}, sinks...)
	}

	// LevelVar lets log_set_level mutate the threshold dynamically
	// without re-wiring the handler.
	p.level = new(slog.LevelVar)
	p.level.Set(cfg.Level)

	p.handler = newFanoutHandler(sinks, &slog.HandlerOptions{
		Level:     p.level,
		AddSource: cfg.AddSource,
	})
	p.logger = slog.New(p.handler)
	p.cfg = cfg

	// Swap the App's logger so middleware.LoggingFn(app.Logger) and any
	// other code calling app.Logger() routes through our sinks. Scoped to
	// this App — no slog.Default rewiring, no stdlib log side effects.
	app.SetLogger(p.logger)

	// Install our own access-log + panic-recovery middleware. Order is
	// access OUTSIDE recovery so the access log's defer reads the
	// status that recovery wrote (500) on a panic. With the reverse
	// ordering the defer would fire mid-unwind and log status=200 even
	// when the response was a 500. These sit INSIDE the framework's
	// default Recovery + Logging — log-recovery catches first because
	// it's the innermost recover.
	app.Use(router.Middleware(accessMiddleware(p.logger, cfg.TrustForwardedFor)))
	app.Use(router.Middleware(recoveryMiddleware(p.logger)))

	if !cfg.DisableLifecycleEvents {
		appName := app.Config.Name
		if appName == "" {
			appName = "app"
		}
		app.OnStart(func(ctx context.Context) error {
			p.logger.LogAttrs(ctx, slog.LevelInfo, "app.start",
				slog.String("app", appName),
				slog.String("go", runtime.Version()),
			)
			return nil
		})
		// OnStopFirst: prepend so log's close fires LAST under the
		// reverse-iteration Stop. Any user OnStop registered before
		// or after RegisterPlugin(log) gets to write its final log
		// lines through live sinks.
		app.OnStopFirst(func() error {
			p.logger.LogAttrs(context.Background(), slog.LevelInfo, "app.stop",
				slog.String("app", appName),
			)
			return p.handler.close()
		})
	} else {
		app.OnStopFirst(func() error {
			return p.handler.close()
		})
	}

	if cfg.EnableMCP {
		if err := p.registerMCPTools(app); err != nil {
			return fmt.Errorf("log: register MCP tools: %w", err)
		}
	}

	return nil
}

// --- fan-out slog.Handler -------------------------------------------------

// fanoutHandler is a slog.Handler that writes each record as one JSON
// line (no leading newline) to every configured sink. It owns the sinks'
// lifecycle.
type fanoutHandler struct {
	sinks []Sink
	opts  *slog.HandlerOptions
	// attrs and group are accumulated state for With/WithGroup chains.
	attrs []slog.Attr
	group string

	mu sync.Mutex
	// metrics is shared across derived handlers so counters on a
	// WithAttrs/WithGroup chain roll up to the original.
	metrics *fanoutMetrics
}

// fanoutMetrics is shared between the root handler and every derived
// handler (via WithAttrs/WithGroup) so counters reflect the whole tree.
type fanoutMetrics struct {
	closedDrops atomic.Uint64
	failedSinks atomic.Uint64 // entries dropped because a sink Write errored
	// lastWriteWarnAt rate-limits stderr emissions for sink-write
	// failures. Without this a wedged disk would flood stderr (and
	// any rate-limited stderr collector behind it) with one ~80 KiB
	// line per failed entry.
	lastWriteWarnAt atomic.Int64 // unix nano
}

// stderrWarnInterval bounds how often the fanout reports a sink-write
// failure to stderr. Reports are counter-aggregated; only ~1 per second
// per process actually hits stderr.
const stderrWarnInterval = time.Second

func newFanoutHandler(sinks []Sink, opts *slog.HandlerOptions) *fanoutHandler {
	if opts == nil {
		opts = &slog.HandlerOptions{}
	}
	return &fanoutHandler{sinks: sinks, opts: opts, metrics: &fanoutMetrics{}}
}

// shouldEmitWriteFailure returns true if a fresh stderr emission is
// allowed under the rate limit. CAS-based so concurrent failing writes
// don't all race past the check.
func (h *fanoutHandler) shouldEmitWriteFailure() bool {
	now := time.Now().UnixNano()
	last := h.metrics.lastWriteWarnAt.Load()
	if last != 0 && time.Duration(now-last) < stderrWarnInterval {
		return false
	}
	return h.metrics.lastWriteWarnAt.CompareAndSwap(last, now)
}

func (h *fanoutHandler) Enabled(_ context.Context, l slog.Level) bool {
	min := slog.LevelInfo
	if h.opts != nil && h.opts.Level != nil {
		min = h.opts.Level.Level()
	}
	return l >= min
}

func (h *fanoutHandler) Handle(ctx context.Context, r slog.Record) error {
	// We delegate to a private slog.JSONHandler bound to a buffer so we
	// get the exact slog JSON encoding (including time format,
	// AddSource, ReplaceAttr, etc.) without re-implementing it.
	var buf bytes.Buffer
	enc := slog.NewJSONHandler(&buf, h.opts)
	// Replay accumulated attrs/group so With chains work.
	var attached slog.Handler = enc
	if len(h.attrs) > 0 {
		attached = attached.WithAttrs(h.attrs)
	}
	if h.group != "" {
		attached = attached.WithGroup(h.group)
	}
	if err := attached.Handle(ctx, r); err != nil {
		return err
	}
	// slog.JSONHandler writes one line terminated by '\n'; strip the
	// newline so sinks can decide their own framing.
	entry := bytes.TrimRight(buf.Bytes(), "\n")
	if len(entry) == 0 {
		return nil
	}

	// Snapshot sinks under the lock then release it before writing —
	// each sink has its own synchronization (fileSink.mu, webhookSink's
	// queue mu), so holding h.mu across a wedged disk write would
	// serialize EVERY slog call in the process. The slice header is
	// safe to read after unlock because sinks is never mutated after
	// construction.
	h.mu.Lock()
	sinks := h.sinks
	h.mu.Unlock()

	var errs []error
	for _, s := range sinks {
		// Copy per sink: webhook sink may retain the slice in a batch
		// buffer beyond this call. File sinks could share, but the
		// extra alloc per entry is the price of safety here.
		cp := make([]byte, len(entry))
		copy(cp, entry)
		if err := s.Write(cp); err != nil {
			// slog.Logger.LogAttrs ignores handler errors (stdlib
			// contract). Fall back to stderr so a wedged sink isn't
			// silent — operators need at least one path to see that
			// logging is broken. ErrSinkClosed is expected post-Stop
			// and gets a quieter shape.
			if errors.Is(err, ErrSinkClosed) {
				// Post-Stop drop. Tracked via an atomic counter and
				// surfaced once on close() so operators chasing
				// "where did my worker's shutdown log go?" have a
				// signal without per-line stderr spam.
				h.metrics.closedDrops.Add(1)
			} else {
				h.metrics.failedSinks.Add(1)
				// Rate-limit stderr emissions to ~1/sec. journald and
				// similar collectors throttle stderr anyway; flooding
				// would push out legitimate logs from elsewhere.
				if h.shouldEmitWriteFailure() {
					// Truncate the entry to a short preview so even
					// the throttled path can't write multi-KB lines.
					preview := entry
					if len(preview) > 256 {
						preview = preview[:256]
					}
					fmt.Fprintf(os.Stderr, "log: sink write failed: %v (preview: %s …)\n", err, preview)
				}
			}
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (h *fanoutHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	combined := make([]slog.Attr, 0, len(h.attrs)+len(attrs))
	combined = append(combined, h.attrs...)
	combined = append(combined, attrs...)
	return &fanoutHandler{
		sinks:   h.sinks,
		opts:    h.opts,
		attrs:   combined,
		group:   h.group,
		metrics: h.metrics, // shared pointer so counters aggregate
	}
}

func (h *fanoutHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	g := h.group
	if g == "" {
		g = name
	} else {
		g = g + "." + name
	}
	return &fanoutHandler{
		sinks:   h.sinks,
		opts:    h.opts,
		attrs:   h.attrs,
		group:   g,
		metrics: h.metrics,
	}
}

// close flushes and closes every sink. Idempotent guard not provided —
// App.Stop calls it once. Surfaces the post-Stop drop counter so an
// operator chasing missing shutdown logs has at least one signal.
func (h *fanoutHandler) close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if d := h.metrics.closedDrops.Load(); d > 0 {
		fmt.Fprintf(os.Stderr, "log: dropped %d log entries after sinks were closed (post-Stop writes)\n", d)
	}
	if f := h.metrics.failedSinks.Load(); f > 0 {
		fmt.Fprintf(os.Stderr, "log: %d log entries dropped due to sink write failures over the run\n", f)
	}
	var errs []error
	for _, s := range h.sinks {
		if err := s.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// --- helpers --------------------------------------------------------------

// stateDir returns the OS-conventional state directory for app data.
// Honors XDG_STATE_HOME on linux; falls back to ~/.local/state.
func stateDir(appName string) (string, error) {
	if appName == "" {
		return "", fmt.Errorf("log: stateDir requires a non-empty app name")
	}
	if x := os.Getenv("XDG_STATE_HOME"); x != "" {
		return filepath.Join(x, appName), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", appName), nil
}

