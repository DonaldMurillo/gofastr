package live

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"sync"

	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/core/openapi"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework"
	kilndb "github.com/DonaldMurillo/gofastr/kiln/db"
	"github.com/DonaldMurillo/gofastr/kiln/journal"
	kilnrender "github.com/DonaldMurillo/gofastr/kiln/render"
	"github.com/DonaldMurillo/gofastr/kiln/world"
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
	stamp        string // kilnrender.WorldStamp of the world app was built from
	bus          *Broadcaster
	aux          *router.Router
	sseSeats     sseSeatRegistry
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
// SetFallbackHTML, the most recent setter wins.
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
	l.aux.NotFound(http.HandlerFunc(l.serveApp))
	if err := l.rebuild(); err != nil {
		return nil, err
	}
	// The runtime DB is journal-derived state: boot rebuilds it (drop,
	// migrate, seed) so a restart over the same journal serves the rows
	// the log says the world has — seeds used to exist only in the
	// process that wrote them. A database that cannot be rebuilt fails
	// the boot: serving it would show rows the journal does not hold.
	if err := l.rederiveDB(); err != nil {
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
// journal, so a poison entry that fails the rebuild never reaches the durable
// log (which would re-fail on every restart). On any failure the pre-entry
// state is restored by replaying the (still-unchanged) journal.
func (l *Live) Apply(e journal.Entry) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	// A delete_entity side effect must drop the entity's table, and the
	// rebuild below removes the entity from the registry, so capture the
	// registry-derived table name before the world is mutated.
	dropTable := ""
	if e.Kind == journal.KindWorldEdit && e.Op == journal.OpDeleteEntity {
		var p journal.DeleteEntityPayload
		if err := e.Decode(&p); err == nil && l.app != nil {
			for _, ent := range l.app.Registry.All() {
				if ent.GetName() == p.Name {
					dropTable = ent.GetTable()
					break
				}
			}
		}
	}
	// update_entity and delete_field can take a column off a table or
	// relax one, which the additive migrate never does: keep the fields
	// as they stand, so the side effect sees what the edit changed.
	// delete_field shifts the field slice in place, hence the clone.
	cut := l.columnCut(e)
	// An add_seed is journaled with the ids its rows will get, so a
	// replay inserts the same ones; everything below sees the stamped
	// entry.
	e = l.stampSeed(e)
	if err := journal.Apply(l.sess, e); err != nil {
		// A refused entry may have changed the session before it failed
		// (journal.Apply is not transactional), and nothing durable
		// records that change: put the session back to the journal.
		l.restoreFromJournal()
		return fmt.Errorf("kiln/live: apply to session: %w", err)
	}
	// Validate by rebuilding BEFORE the durable Append. If the entry can't be
	// rebuilt, roll the in-memory session back to the journal (which does not
	// yet contain e) and surface the error, nothing was persisted.
	if err := l.rebuild(); err != nil {
		l.restoreFromJournal()
		return fmt.Errorf("kiln/live: rebuild: %w", err)
	}
	// Side effects run here too, still BEFORE the durable Append: a seed
	// the DB cannot take (missing table, hostile ident) or a table that
	// cannot drop must leave nothing behind — not the journal entry, not
	// the in-memory world, not the rows. The old order (Append, then
	// side effects) reported failure with the entry already durable, so
	// every restart and every freeze replayed a seed the runtime itself
	// refused to apply.
	if err := l.applySideEffects(e, dropTable, cut); err != nil {
		l.restoreFromJournal()
		// A refused seed rolled back its own transaction, a refused
		// table rebuild put its table back, and a refused drop (a
		// foreign key still pointing at the table) never took it, so the
		// DB still matches the journal. Rederiving drops every table,
		// and with them the CRUD rows the journal never recorded.
		// The edit fails either way; a re-derive failure joins it so the
		// caller sees the database no longer matches the journal.
		var dbErr error
		if e.Op != journal.OpAddSeed && !errors.Is(err, kilndb.ErrTableRestored) {
			dbErr = l.rederiveDB()
		}
		return errors.Join(fmt.Errorf("kiln/live: side-effects: %w", err), dbErr)
	}
	if _, err := l.journal.Append(e); err != nil {
		// Rebuild succeeded but the durable write failed, the in-memory
		// session is now ahead of the journal. Roll back so state never
		// outlives the durable record; re-derive the DB too, the side
		// effect already ran against it.
		l.restoreFromJournal()
		return errors.Join(fmt.Errorf("kiln/live: append to journal: %w", err), l.rederiveDB())
	}
	l.bus.Send(Event{
		EntryID: e.ID,
		Kind:    string(e.Kind),
		Op:      string(e.Op),
		Summary: summarizeEntry(e),
	})
	return nil
}

// stampSeed returns an add_seed entry whose rows carry the ids (and
// declared auto timestamps) ApplySeeds would otherwise generate afresh
// on every replay: kilnrender.StampSeed. Without it a restart gave each
// seeded row a new id, and a later seed naming the old one (a book's
// author_id) failed its foreign key and was skipped. An entry that is
// not an add_seed, or one that does not decode or stamp, comes back as
// it was: journal.Apply and ApplySeeds report that failure.
func (l *Live) stampSeed(e journal.Entry) journal.Entry {
	if e.Kind != journal.KindWorldEdit || e.Op != journal.OpAddSeed {
		return e
	}
	var p journal.AddSeedPayload
	if err := e.Decode(&p); err != nil || p.Seed == nil {
		return e
	}
	stamped, err := kilnrender.StampSeed(l.sess.World.Entities, p.Seed)
	if err != nil || stamped == p.Seed {
		return e
	}
	p.Seed = stamped
	raw, err := json.Marshal(p)
	if err != nil {
		return e
	}
	e.Payload = raw
	return e
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
// not part of world state: agent_turn_started, agent_turn_ended,
// session_reset, etc. Safe to call concurrently.
func (l *Live) Notify(kind, summary string) {
	l.bus.Send(Event{Kind: kind, Summary: summary})
}

// applySideEffects runs the DB-level side effects of an entry: a seed
// inserts its rows, a delete_entity drops the entity's table. It runs
// BEFORE the durable Append (validation stage), so a failure rolls the
// session back and re-derives the DB leaving nothing durable behind.
func (l *Live) applySideEffects(e journal.Entry, dropTable string, cut *columnCut) error {
	if e.Kind != journal.KindWorldEdit {
		return nil
	}
	if l.app == nil || l.app.DB == nil {
		return nil
	}
	switch e.Op {
	case journal.OpAddSeed:
		var p journal.AddSeedPayload
		if err := e.Decode(&p); err != nil {
			return err
		}
		if p.Seed == nil {
			return nil
		}
		w := &world.World{Entities: l.sess.World.Entities, Seeds: []*world.Seed{p.Seed}}
		return kilnrender.ApplySeeds(l.app.DB, w)
	case journal.OpDeleteEntity:
		if dropTable == "" {
			return nil
		}
		return kilndb.DropTable(l.app.DB, dropTable)
	case journal.OpDeleteField, journal.OpUpdateEntity:
		return l.convergeColumns(cut)
	}
	return nil
}

// columnCut is an entity's fields before an update_entity or
// delete_field.
type columnCut struct {
	entity string
	before []world.Field
}

// columnCut records the fields of the entity e edits, or nil when e is
// not an edit that can take columns away.
func (l *Live) columnCut(e journal.Entry) *columnCut {
	if e.Kind != journal.KindWorldEdit {
		return nil
	}
	var name string
	switch e.Op {
	case journal.OpDeleteField:
		var p journal.DeleteFieldPayload
		if e.Decode(&p) != nil {
			return nil
		}
		name = p.Entity
	case journal.OpUpdateEntity:
		var p journal.UpdateEntityPayload
		if e.Decode(&p) != nil || p.Entity == nil {
			return nil
		}
		name = p.Entity.Name
	default:
		return nil
	}
	ent, ok := l.sess.World.Entities[name]
	if !ok || ent == nil {
		return nil
	}
	return &columnCut{entity: name, before: slices.Clone(ent.Fields)}
}

// convergeColumns brings the edited entity's table to what the registry
// declares when the edit took a field away or relaxed one: a dropped
// field's column goes, and a field that is no longer required or unique
// loses its NOT NULL or UNIQUE, which needs the table rebuilt.
func (l *Live) convergeColumns(cut *columnCut) error {
	if cut == nil {
		return nil
	}
	after, ok := l.sess.World.Entities[cut.entity]
	if !ok || after == nil {
		return nil
	}
	table := ""
	for _, ent := range l.app.Registry.All() {
		if ent.GetName() == cut.entity {
			table = ent.GetTable()
			break
		}
	}
	if table == "" {
		return nil
	}
	var drop []string
	relaxed := false
	for _, f := range cut.before {
		i := slices.IndexFunc(after.Fields, func(g world.Field) bool { return g.Name == f.Name })
		if i < 0 {
			drop = append(drop, f.Name)
			continue
		}
		g := after.Fields[i]
		if (f.Required && !g.Required) || (f.Unique && !g.Unique) {
			relaxed = true
		}
	}
	if relaxed {
		return kilndb.RebuildTable(l.app.DB, l.app.Registry, table)
	}
	return kilndb.DropColumns(l.app.DB, l.app.Registry, table, drop)
}

// rederiveDB rebuilds the runtime DB from the journal-derived world:
// every user table is dropped, the schema re-migrated, and the world's
// seeds re-applied. The runtime DB is derived state of the journal —
// the boot, reset, and undo paths all funnel here — so afterwards the
// database holds exactly what the journal authorizes, never rows from a
// reset world, an undone seed, or a deleted entity. Caller must hold
// l.mu. A seed the world cannot apply (a degenerate hand-authored
// journal) is skipped with a warning rather than bricking boot: the
// freeze gate refuses that world separately. A drop or migrate that
// fails is returned: the database then does not match the journal.
func (l *Live) rederiveDB() error {
	if l.app == nil || l.app.DB == nil {
		return nil
	}
	db := l.app.DB
	if err := kilndb.DropAllTables(db); err != nil {
		return fmt.Errorf("kiln/live: re-derive: %w", err)
	}
	if err := kilndb.Migrate(db, l.app.Registry); err != nil {
		return fmt.Errorf("kiln/live: re-derive: migrate: %w", err)
	}
	for i, s := range l.sess.World.Seeds {
		if s == nil {
			continue
		}
		w := &world.World{Entities: l.sess.World.Entities, Seeds: []*world.Seed{s}}
		if err := kilnrender.ApplySeeds(db, w); err != nil {
			slog.Warn("kiln/live: re-derive skipped a seed the world refuses",
				"seed", i, "entity", s.Entity, "err", err)
		}
	}
	return nil
}

// Reload discards the in-memory session and rebuilds it from the journal.
// Useful after out-of-band journal writes or recovery from inconsistency.
// The DB is re-derived too: truncation surfaces (reset_session, undo)
// rewrite the world from the log, and the schema plus rows must follow.
func (l *Live) Reload() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.reloadBody()
}

// ErrNothingToUndo is returned by UndoLast when the journal is already
// empty; protocol maps it to its "nothing to undo" validation result.
var ErrNothingToUndo = errors.New("kiln/live: nothing to undo")

// TruncateAndReload drops journal entries past offset n and rebuilds the
// session from the remaining log, holding l.mu across the whole
// operation. The single hold is the point: reset_session and undo used
// to touch the journal outside this mutex, so an Apply parked inside
// journal.Append under l.mu could land its entry AFTER the truncate and
// have the trailing reload replay it — ResetSession reported success
// over a journal that was not empty (pinned by protocol's
// undo/reset security tests). Routing both through here excludes
// in-flight Applies end to end.
func (l *Live) TruncateAndReload(n int) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.journal.TruncateAfter(n); err != nil {
		return err
	}
	return l.reloadBody()
}

// UndoLast reverts the most recent world edit and reloads. The journal
// is cut at that edit, and at the tool_call envelope journaled just
// before it when there is one, so the edit's tool_result and anything
// journaled after it go too. Cutting a fixed one entry was wrong: the
// HTTP tool API wraps every edit in a tool_call and a tool_result, so
// a one-entry cut removed the tool_result, left the edit in the world,
// and still re-derived the DB. A journal with no world edit has
// nothing to undo. The Read→TruncateAfter pair runs under one hold of
// l.mu so the cut is computed against the log a concurrent Append can
// no longer rewrite between the two steps.
func (l *Live) UndoLast() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	entries, err := l.journal.Read()
	if err != nil {
		return err
	}
	cut := undoCut(entries)
	if cut < 0 {
		return ErrNothingToUndo
	}
	// The undone edit's op rides the event, so the reload client knows
	// whether the document shell changed.
	var undone journal.Op
	for _, e := range entries[cut:] {
		if e.Kind == journal.KindWorldEdit {
			undone = e.Op
		}
	}
	if err := l.journal.TruncateAfter(cut); err != nil {
		return err
	}
	if err := l.reloadBody(); err != nil {
		return err
	}
	// The world a visitor's page renders from just changed: tell the bus
	// so the reload client refreshes, as it does for any world edit.
	l.bus.Send(Event{Kind: EventUndo, Op: string(undone)})
	return nil
}

// EventUndo is the bus event UndoLast sends after the reverted world
// is live. The build-mode reload client refreshes the page on it.
const EventUndo = "undo"

// undoCut returns how many leading entries survive an undo, or -1 when
// the log holds no world edit. The cut lands on the last world edit, or
// on its tool_call envelope when the HTTP tool API journaled one
// immediately before it.
func undoCut(entries []journal.Entry) int {
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Kind != journal.KindWorldEdit {
			continue
		}
		if i > 0 && entries[i-1].Kind == journal.KindToolCall {
			return i - 1
		}
		return i
	}
	return -1
}

// reloadBody is the shared boot/reload sequence: replay the journal,
// rebuild the app, then re-derive the DB from the replayed world.
// Caller must hold l.mu.
func (l *Live) reloadBody() error {
	sess, err := journal.Replay(l.journal)
	if err != nil {
		return fmt.Errorf("kiln/live: reload replay: %w", err)
	}
	l.sess = sess
	if err := l.rebuild(); err != nil {
		return err
	}
	// Reset/undo re-derive: truncation rewrites the world from the log,
	// and the DB must follow — the old schema and rows from before the
	// truncation would otherwise survive (re-adding an entity resurrects
	// the previous world's data through the public CRUD surface). A
	// rebuild that fails is the caller's error: reset_session used to
	// answer OK over a database that still held the old rows.
	return l.rederiveDB()
}

// rebuild constructs a new framework.App from the current world. Caller
// must hold l.mu. If the host wired a DB, AutoMigrate runs after Apply
// so live entity edits propagate to the schema.

func (l *Live) rebuild() (err error) {
	// A world edit reaches net/http's pattern parser and framework
	// App.Mount, and both PANIC on input they cannot register: a path
	// with no leading slash, a malformed wildcard, a screen path that
	// collides with an entity's CRUD mount, a dynamic route whose screen
	// cannot take params. The tool API that produces these world edits is
	// unauthenticated, and a panic here escaped Apply entirely: the
	// rollback below never ran, the poison stayed in the in-memory world,
	// and every later Apply re-panicked at the same registration until
	// the process restarted. New() had the same problem on a journal
	// already containing one, so restarting did not help either.
	//
	// A rebuild failure is exactly what Apply is structured to handle, so
	// turn the panic into one. This does not paper over a bug in our own
	// code: the recovered value is reported verbatim as the rebuild
	// error, so a genuine nil-deref surfaces with its message and stack
	// origin rather than being swallowed.
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("rebuild panicked: %v", r)
		}
	}()
	app := l.factory()
	if err := kilnrender.Apply(app, l.sess.World); err != nil {
		return err
	}
	if app.DB != nil && len(app.Registry.All()) > 0 {
		if err := kilndb.Migrate(app.DB, app.Registry); err != nil {
			return fmt.Errorf("auto-migrate: %w", err)
		}
	}
	// Mount OpenAPI + the API docs page after the registry is populated.
	// Live bypasses framework.App.Start, which is where the framework would
	// normally attach these, wire them here so they're always available.
	if len(app.Registry.All()) > 0 {
		appName := app.Config.Name
		if appName == "" {
			appName = "Kiln"
		}
		// The app's own document: its api_prefix'd paths, its json_case
		// keys and the CRUD routes registration mounted, the same one
		// App.Start would serve.
		spec := app.OpenAPISpec(appName)
		// Kiln is build-mode tooling: the in-studio API browser is part
		// of the developer experience and must remain reachable without
		// the auth chain the framework adds by default. PublicHandler
		// serves the spec without the auth gate.
		app.Router().Get("/openapi.json", openapi.PublicHandler(spec))
		// public=true to match the spec above. Live has no auth chain,
		// so a gated docs page here would 401 for everyone, always.
		app.Router().Get("/api/docs/", openapi.DocsHandler(spec, "/api/docs", true))
	}
	l.app = app
	l.stamp = kilnrender.WorldStamp(l.sess.World)
	return nil
}

// Session returns the current session. The caller must not mutate it.
//
// The lock is released when this returns, so the pointer is only safe
// on goroutines that are serialized with Apply (the agent's tool
// handlers, which read-then-Apply in sequence). Concurrent HTTP
// readers (panels, status endpoints) race Apply through this pointer,
// they must use ReadSession instead.
func (l *Live) Session() *journal.Session {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.sess
}

// ReadSession runs fn while holding the session read lock, so the
// session cannot be mutated by a concurrent Apply for the duration.
// fn must not let the pointer escape and must not call Apply (that
// would deadlock on the same lock).
func (l *Live) ReadSession(fn func(*journal.Session)) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	fn(l.sess)
}

// App returns the current rebuilt framework.App. Callers can use this
// to reach app.MCP (per-entity MCP tools auto-registered when an
// entity has mcp:true) or app.Router() for advanced wiring. Note: the
// pointer changes on every Apply rebuild, don't cache it.
func (l *Live) App() *framework.App {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.app
}

// Journal returns the underlying journal.
func (l *Live) Journal() journal.Journal { return l.journal }

// Subscribe returns a channel of events and an unsubscribe function.
func (l *Live) Subscribe() (<-chan Event, func()) {
	return l.bus.Subscribe()
}

// securityHeaders is the framework default chain's header set with one
// kiln-specific allowance: the fallback host page ships an inline
// <style> block (every value a theme token), so style-src allows
// 'unsafe-inline'. Scripts stay external-only under default-src 'self'.
// Without this wrapper every kiln-served response (panel widget, JSON
// state, the fallback document) skips the nosniff/XFO/CSP discipline
// every framework page rides by default.
var securityHeaders = middleware.SecurityHeaders(middleware.SecurityHeadersConfig{
	ContentSecurityPolicy: "default-src 'self'; style-src 'self' 'unsafe-inline'; " +
		"img-src 'self' data:; object-src 'none'; form-action 'self'; " +
		"frame-ancestors 'none'; base-uri 'self'",
})

// ServeHTTP first tries the auxiliary router (kiln-internal paths),
// falling through to the current app's router on 404. The whole tree is
// wrapped in the security-header middleware so aux routes and the HTML
// fallback carry the same headers the framework default chain sets.
func (l *Live) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Identity marker: every response the kiln runtime serves carries
	// it, so a readiness probe can tell "kiln answered" from "some
	// process that won the port race answered" (cmd/kiln's waitReady
	// refuses to export KILN_URL at an impostor without it).
	w.Header().Set("X-Kiln-Server", "kiln")
	securityHeaders(http.Handler(l.aux)).ServeHTTP(w, r)
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
