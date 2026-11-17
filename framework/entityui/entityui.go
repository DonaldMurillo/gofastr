// Package entityui draws an entity's list and record screens from its
// schema, Display config and States. One *UI serves one app and mounts no
// routes: a builder is a component rendered inside the page that uses it.
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

// Host is one app's view of its entities; App.EntityUI implements it.
type Host interface {
	// Registry holds the app's entities.
	Registry() entity.Registry
	// Crud returns the entity's in-process CRUD handler, wired as the HTTP routes wire theirs.
	Crud(e *entity.Entity) (*crud.CrudHandler, error)
	// APIPath is the base of the entity's REST routes, false when it mounts none.
	APIPath(e *entity.Entity) (string, bool)
	// Translator is the app's catalog; nil when the app has none.
	Translator() *i18n.Translator
	// Audit reads audit rows; nil hides the Activity tab.
	Audit() AuditReader
}

// AuditReader reads one record's audit trail, newest first, at most limit rows.
type AuditReader interface {
	Trail(ctx context.Context, entity, recordID string, limit int) ([]AuditEntry, error)
}

// AuditEntry is one audit row as the Activity tab draws it.
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
	host       Host
	ext        Extensions
	views      SavedViewStore
	recordPath func(e *entity.Entity) (string, bool)
	createPath func(e *entity.Entity) (string, bool)
	actorName  func(ctx context.Context, id string) string
	now        func() time.Time
	screens    *recordScreens
	// scriptlessEdits points edit forms' native action at SaveHandler.
	scriptlessEdits bool
}

// New checks ext against the app's entities and returns the app's UI. It
// refuses, naming the offender, an unknown entity, view, kind or button
// variant, a view with neither Where nor Filter, an invalid, duplicate or
// built-in tab or action key, an action with no Run, a Kind with neither
// Input nor Cell, and Jobs on a Host that is not a BulkHost.
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
	u := &UI{host: h, ext: ext.clone(), now: time.Now}
	if sh, ok := h.(ScreenHost); ok {
		u.screens = &recordScreens{host: sh}
	}
	return u, nil
}

// WithAPIPath returns a UI that points every write (save, delete, moves,
// bulk, export) at path(e); false draws the entity read-only. Reads are unchanged.
func (u *UI) WithAPIPath(path func(e *entity.Entity) (string, bool)) *UI {
	h := apiPathHost{Host: u.host, path: path}
	c := *u
	c.host = h
	if bh, ok := u.host.(BulkHost); ok {
		c.host = apiPathBulkHost{apiPathHost: h, BulkHost: bh}
	}
	return &c
}

// WithRecordPath returns a UI whose list cells link a relation's title to
// path(e) + "/" + id; false leaves the cell text. A nil path changes nothing.
func (u *UI) WithRecordPath(path func(e *entity.Entity) (string, bool)) *UI {
	if path == nil {
		return u
	}
	c := *u
	c.recordPath = path
	return &c
}

// WithCreatePath returns a UI whose create links point to path(e), a path
// with no query or fragment; false or "" hides them. The default is Base +
// "/create"; a nil path changes nothing.
func (u *UI) WithCreatePath(path func(e *entity.Entity) (string, bool)) *UI {
	if path == nil {
		return u
	}
	c := *u
	c.createPath = path
	return &c
}

func (u *UI) createScreenPath(e *entity.Entity, base string) string {
	if u.createPath != nil {
		path, ok := u.createPath(e)
		if !ok {
			return ""
		}
		return path
	}
	return under(base, "create")
}

// WithSavedViews returns a UI whose lists can keep named saved views in
// store (ListBuilder.SavedViews); a nil store changes nothing.
func (u *UI) WithSavedViews(store SavedViewStore) *UI {
	if store == nil {
		return u
	}
	c := *u
	c.views = store
	return &c
}

// WithActorName returns a UI whose Activity tab names each actor by
// name(ctx, id); "" or a panic shows the id. A nil name changes nothing.
func (u *UI) WithActorName(name func(ctx context.Context, id string) string) *UI {
	if name == nil {
		return u
	}
	c := *u
	c.actorName = name
	return &c
}

// WritePath reports where this UI's screens send e's writes; false means
// they draw e read-only.
func (u *UI) WritePath(e *entity.Entity) (string, bool) {
	if e == nil {
		return "", false
	}
	return u.host.APIPath(e)
}

type apiPathHost struct {
	Host
	path func(e *entity.Entity) (string, bool)
}

func (h apiPathHost) APIPath(e *entity.Entity) (string, bool) { return h.path(e) }

type apiPathBulkHost struct {
	apiPathHost
	BulkHost
}

func (u *UI) entityFor(name string) (*entity.Entity, error) {
	e, err := u.host.Registry().Get(name)
	if err != nil {
		return nil, fmt.Errorf("entityui: unknown entity %q", name)
	}
	return e, nil
}

var builtinTabs = []string{"edit", "related", "activity", "api"}

func isBuiltinTab(key string) bool { return slices.Contains(builtinTabs, key) }

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

func kindText(ctx context.Context, entityName, field string, text func() string) (out string) {
	defer func() {
		if r := recover(); r != nil {
			//gofastr:allow(recoverlog) logs only the panic value's type: its text may carry record contents
			slog.ErrorContext(ctx, "entityui: kind text panicked", "entity", entityName, "field", field, "panic", fmt.Sprintf("%T", r))
			out = ""
		}
	}()
	return text()
}
