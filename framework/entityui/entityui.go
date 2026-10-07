// Package entityui draws an entity's list and record screens from its
// schema, its Display config and its States, for generated apps and the
// admin alike.
//
// One *UI serves one app. The root package builds it (App.EntityUI), so
// this package imports only the layers below the root and nothing in it
// is global: two apps in one test process stay apart.
//
// entityui mounts no routes and no islands. A builder is a component
// rendered inside the page that uses it:
//
//   - Reads run through the entity's in-process CRUD handler with the
//     caller's own context, after the read gates the JSON API applies
//     (CanReadScoped for the list, CanReadRecordScoped for a record). The
//     page's own route policy has already run, so a list on a page is never
//     wider than the page or the API.
//   - In-page state (sort, page, search, filter, view) rides the page's own
//     query string. Links and GET forms navigate; the runtime fetches the
//     screen partial and keeps the shell (Hard rule 1).
//   - Writes go to the entity's REST routes as form RPCs (data-cui-rpc),
//     whose answer re-fetches the page (data-cui-rpc-navigate). A builder
//     never allows a write the API refuses. An entity with no CRUD routes
//     renders read-only.
//   - Moves post the transition route, POST <api>/<entity>/<id>/transitions/<key>.
//
// Every name a builder or an Extensions value uses is checked: at New for
// Extensions, and when the builder renders for builder options, where a bad
// name fails that slot with a generic message and a log line.
package entityui

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// Host is one app's view of its entities. App.EntityUI implements it; a
// test may implement it over a bare registry and database.
type Host interface {
	// Registry holds the app's entities.
	Registry() entity.Registry
	// Crud returns the entity's in-process CRUD handler, wired exactly as
	// the HTTP routes wire theirs (hooks, audit, events, JSON casing).
	Crud(e *entity.Entity) (*crud.CrudHandler, error)
	// APIPath is the base of the entity's REST routes ("/api/invoices"),
	// and false when the entity mounts none.
	APIPath(e *entity.Entity) (string, bool)
	// Translator is the app's catalog; nil when the app has none.
	Translator() *i18n.Translator
	// Audit reads audit rows; nil when the app keeps no audit log, and
	// the Activity tab is then not drawn.
	Audit() AuditReader
}

// AuditReader reads one record's audit trail, newest first, at most limit
// rows. The record's own read gate has passed before it is called.
type AuditReader interface {
	Trail(ctx context.Context, entity, recordID string, limit int) ([]AuditEntry, error)
}

// AuditEntry is one audit row as the Activity tab draws it. Before and
// After hold the redacted values the audit log stored; entityui strips
// Hidden and masked fields again before drawing a diff.
type AuditEntry struct {
	At        time.Time
	Actor     string
	Operation string
	Reason    string
	Before    map[string]any
	After     map[string]any
}

// UI draws one app's entity screens. Build it once at boot with New.
type UI struct {
	host Host
	ext  Extensions
	// views keeps saved list views, nil when the app keeps none.
	views SavedViewStore
	// recordPath is the base of an entity's record screens, nil when
	// the UI links no relation to its record.
	recordPath func(e *entity.Entity) (string, bool)
	// now is the clock queued runs lease and finish by.
	now func() time.Time
}

// New checks ext against the app's entities and returns the app's UI.
// It refuses, naming the offender:
//
//   - an Extensions.Entities key that names no registered entity;
//   - a view func for a key the entity's Display declares no view for,
//     and a declared view with neither a Where nor a registered Filter;
//   - a FieldDisplay.Input naming a kind Extensions.Kinds does not hold;
//   - a tab or action key outside the key grammar (entity.ValidKey),
//     a duplicate one, or a tab key that shadows a built-in tab
//     (edit, related, activity, api);
//   - an action with no Run, or a Kind with no Input and no Cell.
func New(h Host, ext Extensions) (*UI, error) {
	if h == nil {
		return nil, fmt.Errorf("entityui: New needs a Host")
	}
	if err := ext.check(h.Registry()); err != nil {
		return nil, err
	}
	if ext.Jobs != nil {
		if bh, ok := h.(BulkHost); !ok || bh.BulkStore() == nil {
			return nil, fmt.Errorf("entityui: Extensions.Jobs needs a Host that keeps bulk snapshots (BulkHost with a BulkStore)")
		}
	}
	return &UI{host: h, ext: ext.clone(), now: time.Now}, nil
}

// WithAPIPath returns a UI that draws the same screens with the same
// Extensions but points every write (save, delete, moves, bulk, export)
// at path(e) instead of the entity's REST routes. A back office uses it
// to send writes through routes it gates itself, the way battery/admin
// mounts the CRUD handler's write routes behind its own gate. path
// answers false for an entity whose screens should draw read-only.
// Reads are unchanged: they run in process under the caller's context.
func (u *UI) WithAPIPath(path func(e *entity.Entity) (string, bool)) *UI {
	h := apiPathHost{Host: u.host, path: path}
	c := *u
	c.host = h
	if bh, ok := u.host.(BulkHost); ok {
		c.host = apiPathBulkHost{apiPathHost: h, BulkHost: bh}
	}
	return &c
}

// WithRecordPath returns a UI whose list cells link a relation's title
// to the related record, at path(e) + "/" + id, as a chip. path answers
// false for an entity whose records have no screen here, and the cell
// stays text. A link is drawn only for a title the caller's own read
// of the related entity returned, and the record screen behind it runs
// its own read gate. A nil path returns the UI unchanged.
func (u *UI) WithRecordPath(path func(e *entity.Entity) (string, bool)) *UI {
	if path == nil {
		return u
	}
	c := *u
	c.recordPath = path
	return &c
}

// WithSavedViews returns a UI whose lists can keep named saved views in
// store (ListBuilder.SavedViews turns a list on). The same host,
// Extensions and clock ride along — a UI that WithAPIPath rebuilt keeps
// its write paths, so the save and delete forms still post through
// them. The store reads the owner and tenant from the caller's context
// only; nothing about a caller travels from a request into it. A nil
// store returns the UI unchanged.
func (u *UI) WithSavedViews(store SavedViewStore) *UI {
	if store == nil {
		return u
	}
	c := *u
	c.views = store
	return &c
}

// apiPathHost is a Host whose write routes live elsewhere.
type apiPathHost struct {
	Host
	path func(e *entity.Entity) (string, bool)
}

func (h apiPathHost) APIPath(e *entity.Entity) (string, bool) { return h.path(e) }

// apiPathBulkHost keeps the wrapped host's bulk backing.
type apiPathBulkHost struct {
	apiPathHost
	BulkHost
}

// entityFor resolves a builder's entity name.
func (u *UI) entityFor(name string) (*entity.Entity, error) {
	e, err := u.host.Registry().Get(name)
	if err != nil {
		return nil, fmt.Errorf("entityui: unknown entity %q", name)
	}
	return e, nil
}

// builtinTabs are the record tab keys an extension tab may not take.
var builtinTabs = []string{"edit", "related", "activity", "api"}

func isBuiltinTab(key string) bool { return slices.Contains(builtinTabs, key) }

// contain runs build inside a recover and reports a failure as a generic
// slot message and a log line naming the entity and slot, never record
// contents. Every extension call (Build, Filter, Show, Input, Cell, Run)
// and every builder render goes through it.
func contain(ctx context.Context, entityName, slot string, build func() (render.HTML, error)) (out render.HTML) {
	defer func() {
		if r := recover(); r != nil {
			//gofastr:allow(recoverlog) logs only the panic value's type: its text may carry record contents
			slog.ErrorContext(ctx, "entityui: slot panicked", "entity", entityName, "slot", slot, "panic", fmt.Sprintf("%T", r))
			out = slotFailed(ctx)
		}
	}()
	html, err := build()
	if err != nil {
		slog.ErrorContext(ctx, "entityui: slot failed", "entity", entityName, "slot", slot, "error", err)
		return slotFailed(ctx)
	}
	return html
}
