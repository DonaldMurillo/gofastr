package live

import (
	"fmt"
	"net/http"
	"sync"

	"github.com/DonaldMurillo/gofastr/core/openapi"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework"
	kilndb "github.com/DonaldMurillo/gofastr/kiln/db"
	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/world"
	kilnrender "github.com/DonaldMurillo/gofastr/kiln/render"
)

// AppFactory builds a fresh framework.App. Kiln calls it on every rebuild
// so callers can wire any options they want (DB, custom plugins, …).
type AppFactory func() *framework.App

// Live is the runtime coordinator: Session ↔ Journal ↔ App ↔ SSE.
type Live struct {
	mu           sync.RWMutex
	journal      journal.Journal
	sess         *journal.Session
	factory      AppFactory
	app          *framework.App
	deferred     kilnrender.Deferred
	bus          *Broadcaster
	aux          *router.Router
	fallbackHTML string
	fallbackFunc func(*http.Request) string
}

// SetFallbackHTML installs a catch-all HTML response for any request the
// world doesn't claim. Empty disables the fallback (default). cmd/kiln
// wires this to chat.HostHTML() so every URL surfaces the floating
// widget when no page is registered.
func (l *Live) SetFallbackHTML(html string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.fallbackHTML = html
	l.fallbackFunc = nil
}

// SetFallbackFunc installs a per-request fallback renderer. Use when
// the fallback HTML depends on request headers (e.g. host/port for
// rendering correct curl examples). Mutually exclusive with
// SetFallbackHTML — the most recent setter wins.
func (l *Live) SetFallbackFunc(fn func(*http.Request) string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.fallbackFunc = fn
	l.fallbackHTML = ""
}

// New constructs a Live runtime from an existing journal (which may already
// contain entries from a previous session). It replays the journal, builds
// the initial app, and is ready to Apply further entries.
func New(j journal.Journal, factory AppFactory) (*Live, error) {
	if j == nil {
		return nil, fmt.Errorf("kiln/live: nil journal")
	}
	if factory == nil {
		return nil, fmt.Errorf("kiln/live: nil factory")
	}
	sess, err := journal.Replay(j)
	if err != nil {
		return nil, fmt.Errorf("kiln/live: replay: %w", err)
	}
	l := &Live{
		journal: j,
		sess:    sess,
		factory: factory,
		bus:     NewBroadcaster(),
		aux:     router.New(),
	}
	// aux handles kiln-internal paths (chat panel, /.kiln/events, etc.)
	// and falls through to the rebuilt app for everything else.
	l.aux.NotFound(http.HandlerFunc(l.serveApp))
	if err := l.rebuild(); err != nil {
		return nil, err
	}
	return l, nil
}

// Aux returns the auxiliary router, mounted on the same listener as the
// rebuilt app but stable across rebuilds. Use it to register surfaces
// that must survive world mutations (e.g. the chat panel).
func (l *Live) Aux() *router.Router { return l.aux }

// Apply is the single funnel. It applies the entry to the in-memory session,
// validates it by rebuilding the app, and ONLY THEN persists it to the
// journal — so a poison entry that fails the rebuild never reaches the durable
// log (which would re-fail on every restart). On any failure the pre-entry
// state is restored by replaying the (still-unchanged) journal.
func (l *Live) Apply(e journal.Entry) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := journal.Apply(l.sess, e); err != nil {
		return fmt.Errorf("kiln/live: apply to session: %w", err)
	}
	// Validate by rebuilding BEFORE the durable Append. If the entry can't be
	// rebuilt, roll the in-memory session back to the journal (which does not
	// yet contain e) and surface the error — nothing was persisted.
	if err := l.rebuild(); err != nil {
		l.restoreFromJournal()
		return fmt.Errorf("kiln/live: rebuild: %w", err)
	}
	if _, err := l.journal.Append(e); err != nil {
		// Rebuild succeeded but the durable write failed — the in-memory
		// session is now ahead of the journal. Roll back so state never
		// outlives the durable record.
		l.restoreFromJournal()
		return fmt.Errorf("kiln/live: append to journal: %w", err)
	}
	if err := l.applySideEffects(e); err != nil {
		return fmt.Errorf("kiln/live: side-effects: %w", err)
	}
	l.bus.Send(Event{
		EntryID: e.ID,
		Kind:    string(e.Kind),
		Op:      string(e.Op),
		Summary: summarizeEntry(e),
	})
	return nil
}

// restoreFromJournal replays the durable journal into a fresh session and
// rebuilds, discarding any in-memory mutation not yet persisted. Best-effort:
// if replay itself fails the caller should treat the runtime as needing a
// Reload. Caller must hold l.mu.
func (l *Live) restoreFromJournal() {
	if sess, err := journal.Replay(l.journal); err == nil {
		l.sess = sess
		_ = l.rebuild()
	}
}

// Notify broadcasts a synthetic SSE Event without journaling. Use for
// runtime lifecycle signals the panel needs to react to but that are
// not part of world state — agent_turn_started, agent_turn_ended,
// session_reset, etc. Safe to call concurrently.
func (l *Live) Notify(kind, summary string) {
	l.bus.Send(Event{Kind: kind, Summary: summary})
}

// applySideEffects runs DB-level side effects keyed to specific ops that
// can't be rebuilt idempotently. Today: OpAddSeed inserts the seed rows
// after the rebuild has migrated the schema.
func (l *Live) applySideEffects(e journal.Entry) error {
	if e.Kind != journal.KindWorldEdit {
		return nil
	}
	if e.Op != journal.OpAddSeed {
		return nil
	}
	if l.app == nil || l.app.DB == nil {
		return nil
	}
	var p journal.AddSeedPayload
	if err := e.Decode(&p); err != nil {
		return err
	}
	if p.Seed == nil {
		return nil
	}
	w := &world.World{Seeds: []*world.Seed{p.Seed}}
	return kilnrender.ApplySeeds(l.app.DB, w)
}

// Reload discards the in-memory session and rebuilds it from the journal.
// Useful after out-of-band journal writes or recovery from inconsistency.
func (l *Live) Reload() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	sess, err := journal.Replay(l.journal)
	if err != nil {
		return fmt.Errorf("kiln/live: reload replay: %w", err)
	}
	l.sess = sess
	return l.rebuild()
}

// rebuild constructs a new framework.App from the current world. Caller
// must hold l.mu. If the host wired a DB, AutoMigrate runs after Apply
// so live entity edits propagate to the schema.
func (l *Live) rebuild() error {
	app := l.factory()
	d, err := kilnrender.ApplyDetailed(app, l.sess.World)
	if err != nil {
		return err
	}
	if app.DB != nil && len(app.Registry.All()) > 0 {
		if err := kilndb.Migrate(app.DB, app.Registry); err != nil {
			return fmt.Errorf("auto-migrate: %w", err)
		}
	}
	// Mount OpenAPI + Swagger UI after the registry is populated. Live
	// bypasses framework.App.Start, which is where the framework would
	// normally attach these — wire them here so they're always available.
	if len(app.Registry.All()) > 0 {
		appName := app.Config.Name
		if appName == "" {
			appName = "Kiln"
		}
		spec := framework.EntityOpenAPI(app.Registry, appName, "1.0.0")
		// Kiln is build-mode tooling: the in-studio API browser is part
		// of the developer experience and must remain reachable without
		// the auth chain the framework adds by default. PublicHandler
		// serves the spec without the auth gate.
		app.Router().Get("/openapi.json", openapi.PublicHandler(spec))
		app.Router().Get("/api/docs/", openapi.SwaggerUIHandler(spec, "/api/docs"))
	}
	l.app = app
	l.deferred = d
	return nil
}

// Session returns the current session. The caller must not mutate it.
func (l *Live) Session() *journal.Session {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.sess
}

// App returns the current rebuilt framework.App. Callers can use this
// to reach app.MCP (per-entity MCP tools auto-registered when an
// entity has mcp:true) or app.Router() for advanced wiring. Note: the
// pointer changes on every Apply rebuild — don't cache it.
func (l *Live) App() *framework.App {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.app
}

// Journal returns the underlying journal.
func (l *Live) Journal() journal.Journal { return l.journal }

// Deferred returns the most recent Deferred report from rebuild.
func (l *Live) Deferred() kilnrender.Deferred {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.deferred
}

// Subscribe returns a channel of events and an unsubscribe function.
func (l *Live) Subscribe() (<-chan Event, func()) {
	return l.bus.Subscribe()
}

// ServeHTTP first tries the auxiliary router (kiln-internal paths),
// falling through to the current app's router on 404.
func (l *Live) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	l.aux.ServeHTTP(w, r)
}

// serveApp delegates to the rebuilt app. Used as the aux router's NotFound.
// If the app router 404s on an HTML request, we serve the fallback host
// page so the floating widget is reachable from any URL.
func (l *Live) serveApp(w http.ResponseWriter, r *http.Request) {
	l.mu.RLock()
	app := l.app
	fallback := l.fallbackHTML
	fallbackFn := l.fallbackFunc
	l.mu.RUnlock()

	if fallbackFn != nil && fallback == "" {
		fallback = fallbackFn(r)
	}

	if fallback != "" && wantsHTML(r) {
		rec := newCapturingRecorder()
		app.Router().ServeHTTP(rec, r)
		if rec.code == http.StatusNotFound {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			fmt.Fprint(w, fallback)
			return
		}
		rec.flushTo(w)
		return
	}
	app.Router().ServeHTTP(w, r)
}

// wantsHTML returns true if the request prefers HTML (typical browser nav).
// We match on Accept rather than path so JSON tools and curl calls keep
// getting clean 404s.
func wantsHTML(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	accept := r.Header.Get("Accept")
	if accept == "" {
		return true // no header → browser-style default
	}
	for _, part := range splitComma(accept) {
		t := part
		if i := indexByte(t, ';'); i >= 0 {
			t = t[:i]
		}
		t = trimSpace(t)
		if t == "text/html" || t == "*/*" {
			return true
		}
	}
	return false
}

// minimal local helpers to avoid pulling strings just for these.
func splitComma(s string) []string {
	out := []string{}
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}
func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}
func trimSpace(s string) string {
	i, j := 0, len(s)
	for i < j && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	for j > i && (s[j-1] == ' ' || s[j-1] == '\t') {
		j--
	}
	return s[i:j]
}
