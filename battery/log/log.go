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
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/dev"
	"go.opentelemetry.io/otel/trace"
)

// Config configures the log plugin.
type Config struct {
	// Level is the minimum slog level emitted.
	Level slog.Level

	// Sinks receive every log entry.
	Sinks []Sink

	// DisableLifecycleEvents skips the "app.start" / "app.stop" entries.
	DisableLifecycleEvents bool

	// AddSource adds source file:line to every entry.
	AddSource bool

	// TrustForwardedFor opts in to reading the client IP from the
	// `X-Forwarded-For` / `X-Real-IP` headers in the access log's `remote`
	// field.
	TrustForwardedFor bool

	// and NO_COLOR is unset or empty, so Config{} gives a local dev a
	// to force attachment regardless of TTY or NO_COLOR, or ConsoleOff
	// to skip automatic attachment. The attached sink uses the same TTY
	// and NO_COLOR checks for color in both modes. An explicit ConsoleSink
	// in Sinks can set its own writer and color.
	Console ConsoleMode

	// EnableMCP installs a RingSink + MCP tools (log_recent, log_filter,
	// log_metrics, log_set_level) so a connected agent can debug the running
	// app via the plugin's logger surface.
	EnableMCP bool

	// MCPRingSize is the ring buffer capacity used when EnableMCP is true.
	MCPRingSize int

	// AllowMCPMutation gates the registration of the mutating `log_set_level`
	// tool.
	AllowMCPMutation bool

	// MCPGate replaces the precondition every log MCP tool runs behind,
	// framework.MCPRequireRole("admin") by default.
	MCPGate func(ctx context.Context) error

	// ErrorReporter receives recovered panics (from the plugin's
	// panic-recovery middleware) and any errors app code forwards via
	// Plugin.Reporter().
	ErrorReporter ErrorReporter
}

// Plugin is the log plugin.
type Plugin struct {
	cfg    Config
	logger *slog.Logger

	handler atomic.Pointer[fanoutHandler]

	ring *RingSink

	level *slog.LevelVar

	resolvedFilePath string

	reporter ErrorReporter

	mutationDevImplied bool

	mcpDevImplied bool
}

// New constructs an unregistered plugin.
func New(cfg Config) *Plugin {
	if cfg.Level == 0 {
		cfg.Level = slog.LevelInfo
	}
	return &Plugin{cfg: cfg}
}

// Name implements framework.Plugin.
func (p *Plugin) Name() string { return "log" }

// Reporter returns the configured ErrorReporter (the same one the
// panic-recovery middleware reports to).
func (p *Plugin) Reporter() ErrorReporter {
	if p.reporter == nil {
		return noopReporter{}
	}
	return p.reporter
}

func (p *Plugin) closeReporter(handlerErr error) error {
	if c, ok := p.reporter.(interface{ Close() error }); ok {
		_ = c.Close()
	}
	return handlerErr
}

// Logger returns the *slog.Logger that fans out to every configured sink.
func (p *Plugin) Logger() *slog.Logger { return p.logger }

// Metrics is a point-in-time snapshot of counters surfaced by the log
// plugin.
type Metrics struct {
	// PostStopDrops counts log entries discarded because they arrived after
	// sinks were closed (worker goroutines or stop hooks logging during
	// shutdown).
	PostStopDrops uint64
	// SinkWriteFailures counts log entries dropped because a sink's Write
	// returned a non-ErrSinkClosed error (disk full, network).
	SinkWriteFailures uint64
	// WebhookDropped is the sum of entries dropped from every webhook sink's
	// bounded queue (drop-oldest under backpressure).
	WebhookDropped uint64
	// WebhookGaveUp is the sum of batches given up by every webhook sink
	// after exhausting MaxRetries.
	WebhookGaveUp uint64
}

// Metrics returns a point-in-time snapshot of the plugin's counters.
func (p *Plugin) Metrics() Metrics {
	handler := p.handler.Load()
	if handler == nil {
		return Metrics{}
	}
	m := Metrics{
		PostStopDrops:     handler.metrics.closedDrops.Load(),
		SinkWriteFailures: handler.metrics.failedSinks.Load(),
	}
	for _, s := range handler.sinks {
		if ws, ok := s.(*webhookSink); ok {
			m.WebhookDropped += ws.Dropped()
			m.WebhookGaveUp += ws.GaveUp()
		}
	}
	return m
}

// Init implements framework.Plugin.
func (p *Plugin) Init(app *framework.App) error {
	cfg := p.cfg

	if dev.DevMCPEnabled() {
		p.mcpDevImplied = !cfg.EnableMCP
		cfg.EnableMCP = true
		p.mutationDevImplied = !cfg.AllowMCPMutation
		cfg.AllowMCPMutation = true
	}

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

	switch cfg.Console {
	case ConsoleOff:
	case ConsoleOn:
		sinks = append(sinks, ConsoleSink(ConsoleOpts{}))
	default:
		if shouldColor(os.Stderr) {
			sinks = append(sinks, ConsoleSink(ConsoleOpts{}))
		}
	}

	if cfg.EnableMCP {
		p.ring = NewRingSink(cfg.MCPRingSize)
		sinks = append([]Sink{p.ring}, sinks...)
	}

	p.level = new(slog.LevelVar)
	p.level.Set(cfg.Level)

	handler := newFanoutHandler(sinks, &slog.HandlerOptions{
		Level:     p.level,
		AddSource: cfg.AddSource,
	})
	handler.location = p.resolvedFilePath
	p.handler.Store(handler)
	p.logger = slog.New(handler)
	p.cfg = cfg

	app.SetLogger(p.logger)
	_ = app.Metrics()

	app.Use(router.Middleware(accessMiddleware(p.logger, cfg.TrustForwardedFor)))

	p.reporter = cfg.ErrorReporter
	if p.reporter == nil {
		p.reporter = SlogErrorReporter{Logger: p.logger}
	}
	app.Use(router.Middleware(recoveryMiddleware(p.reporter)))

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
		app.OnStopFirst(func() error {
			p.logger.LogAttrs(context.Background(), slog.LevelInfo, "app.stop",
				slog.String("app", appName),
			)
			return p.closeReporter(handler.close())
		})
	} else {
		app.OnStopFirst(func() error {
			return p.closeReporter(handler.close())
		})
	}

	if cfg.EnableMCP {
		if err := p.registerMCPTools(app); err != nil {
			return fmt.Errorf("log: register MCP tools: %w", err)
		}
	}

	return nil
}

type fanoutHandler struct {
	sinks []Sink
	opts  *slog.HandlerOptions
	steps []handlerStep

	mu       sync.Mutex
	metrics  *fanoutMetrics
	location string
}

// LogLocation reports where this handler's records land: the default file
// sink's path, or "" when the host configured its own sinks.
func (h *fanoutHandler) LogLocation() string { return h.location }

type handlerStep struct {
	attrs []slog.Attr
	group string
}

type fanoutMetrics struct {
	closedDrops     atomic.Uint64
	failedSinks     atomic.Uint64
	lastWriteWarnAt atomic.Int64
}

const stderrWarnInterval = time.Second

func newFanoutHandler(sinks []Sink, opts *slog.HandlerOptions) *fanoutHandler {
	if opts == nil {
		opts = &slog.HandlerOptions{}
	}
	return &fanoutHandler{sinks: sinks, opts: opts, metrics: &fanoutMetrics{}}
}

// allowWarn reports whether a fresh stderr warning may be emitted: true
// at most once per interval. CAS-based so concurrent warnings don't race past.
func allowWarn(last *atomic.Int64, interval time.Duration) bool {
	now := time.Now().UnixNano()
	prev := last.Load()
	if prev != 0 && time.Duration(now-prev) < interval {
		return false
	}
	return last.CompareAndSwap(prev, now)
}

func (h *fanoutHandler) Enabled(_ context.Context, l slog.Level) bool {
	min := slog.LevelInfo
	if h.opts != nil && h.opts.Level != nil {
		min = h.opts.Level.Level()
	}
	return l >= min
}

func (h *fanoutHandler) Handle(ctx context.Context, r slog.Record) error {
	var buf bytes.Buffer
	enc := slog.NewJSONHandler(&buf, h.opts)
	var attached slog.Handler = enc
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		attached = attached.WithAttrs([]slog.Attr{
			slog.String("trace_id", sc.TraceID().String()),
			slog.String("span_id", sc.SpanID().String()),
		})
	}
	for _, st := range h.steps {
		if st.group != "" {
			attached = attached.WithGroup(st.group)
		} else {
			attached = attached.WithAttrs(st.attrs)
		}
	}
	if err := attached.Handle(ctx, r); err != nil {
		return err
	}
	entry := bytes.TrimRight(buf.Bytes(), "\n")
	if len(entry) == 0 {
		return nil
	}

	h.mu.Lock()
	sinks := h.sinks
	h.mu.Unlock()

	var errs []error
	for _, s := range sinks {
		cp := make([]byte, len(entry))
		copy(cp, entry)
		if err := s.Write(cp); err != nil {
			if errors.Is(err, ErrSinkClosed) {
				h.metrics.closedDrops.Add(1)
			} else {
				h.metrics.failedSinks.Add(1)
				if allowWarn(&h.metrics.lastWriteWarnAt, stderrWarnInterval) {
					preview := entry
					if len(preview) > 256 {
						preview = preview[:256]
					}
					fmt.Fprintf(os.Stderr, "log: sink write failed: %v (preview: %s …)\n",
						err, textsafe.StripUnsafe(string(preview)))
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
	return h.with(handlerStep{attrs: slices.Clone(attrs)})
}

func (h *fanoutHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return h.with(handlerStep{group: name})
}

func (h *fanoutHandler) with(st handlerStep) *fanoutHandler {
	steps := make([]handlerStep, 0, len(h.steps)+1)
	steps = append(steps, h.steps...)
	steps = append(steps, st)
	return &fanoutHandler{
		sinks:    h.sinks,
		opts:     h.opts,
		steps:    steps,
		metrics:  h.metrics,
		location: h.location,
	}
}

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

// stateDir returns the per-app state directory under XDG_STATE_HOME
// when it is non-empty, or under ~/.local/state otherwise.
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
