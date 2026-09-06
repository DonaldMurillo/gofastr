package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/framework/ui/resource"
)

// The notes entity and its hooks. Everything here is plain GoFastr: no
// desktop-specific code except the AfterCreate notification, which is
// the point of the example. The same entity, hooks, and screens run in
// the desktop window and behind `--serve`.

// registerNotesEntity declares the one entity. Owner-scoped (hard rule
// 6): every row belongs to the local identity, and SearchFields puts
// both text columns behind ?q=.
func registerNotesEntity(app *framework.App) {
	// the desktop battery installs the local identity middleware (its Init); owner-scoped CRUD answers the window's own requests without battery/auth
	app.Entity("notes", framework.EntityConfig{
		Scope:        &framework.ScopeConfig{OwnerField: "user_id"},
		SearchFields: []string{"title", "body"},
		Fields: []schema.Field{
			{Name: "user_id", Type: schema.String},
			{Name: "title", Type: schema.String, Required: true, Max: new(200.0)},
			{Name: "body", Type: schema.Text},
		},
	})
}

// registerSettingsEntity declares the per-owner settings row: one row
// per local user, created on the first /settings visit. Owner-scoped
// like notes (hard rule 6).
func registerSettingsEntity(app *framework.App) {
	// the desktop battery installs the local identity middleware (its Init); owner-scoped CRUD answers the window's own requests without battery/auth
	app.Entity("settings", framework.EntityConfig{
		Scope: &framework.ScopeConfig{OwnerField: "user_id"},
		Fields: []schema.Field{
			{Name: "user_id", Type: schema.String},
			{Name: "notify_on_save", Type: schema.Bool, Default: true},
			{Name: "export_folder", Type: schema.String},
		},
	})
}

// noteRow is the typed hook payload. The framework hands hooks a
// snake_cased map; the typed wrappers translate through these json
// tags, so camelCase tags are the contract.
type noteRow struct {
	Title string `json:"title"`
}

// registerNotesHooks fires a native notification after a save.
// created_at and updated_at come from the entity's default
// Timestamps=true and are auto-stamped; declaring updated_at by hand as
// a plain timestamp shadowed the automatic one and left it empty.
// ErrUnsupported is the unbundled run (`go run` outside a signed .app,
// or a host without a native shell): the notification is skipped, not
// an error. Anything else is logged at Warn; a failed toast must never
// fail the save.
func registerNotesHooks(app *framework.App, d *desktop.Battery) {
	framework.OnAfterCreate[noteRow](app, "notes", func(ctx context.Context, n *noteRow) error {
		if d == nil {
			return nil
		}
		if !notifyOnSave(ctx, app) {
			return nil
		}
		err := d.Notify(ctx, desktop.Notification{Title: "Saved", Subtitle: "Notes", Body: n.Title})
		var de *desktop.Error
		if err == nil || (errors.As(err, &de) && de.Code == desktop.CodeUnsupported) {
			// Unsupported covers every "this host cannot notify" answer:
			// no bundle, an unsigned bundle, the user declined. None of
			// them is worth a log line on every save.
			return nil
		}
		slog.Warn("desktop-notes: notification failed", "error", err)
		return nil
	})
}

// notifyOnSave reads the owner's settings row (creating nothing): the
// notify_on_save preference, defaulting to true when no row exists
// yet. A read failure also falls back to true, matching the
// notification's history of never failing the save.
func notifyOnSave(ctx context.Context, app *framework.App) bool {
	rows, err := app.MustCrudHandler("settings").ListAll(ctx, crud.ListOptions{Limit: 1})
	if err != nil || len(rows) == 0 {
		return true
	}
	v, present := rows[0]["notifyOnSave"]
	if !present {
		return true
	}
	b, ok := v.(bool)
	if !ok {
		return true
	}
	return b
}

// searchAcrossFields adapts the CrudHandler to resource.Config's
// DataSource seam so the list screen's search box uses the entity's
// SearchFields (title AND body) instead of the resource engine's
// single-column LIKE. It rewrites the engine's `title LIKE ?` filter
// into ListOptions.Search, which the CRUD layer expands across every
// declared SearchField.
type searchAcrossFields struct {
	src resource.DataSource
}

func (s searchAcrossFields) CountAll(ctx context.Context, opts crud.ListOptions) (int, error) {
	return s.src.CountAll(ctx, toSearch(opts))
}

func (s searchAcrossFields) ListAll(ctx context.Context, opts crud.ListOptions) ([]map[string]any, error) {
	return s.src.ListAll(ctx, toSearch(opts))
}

func (s searchAcrossFields) GetOne(ctx context.Context, id string, includes []string) (map[string]any, error) {
	return s.src.GetOne(ctx, id, includes)
}

// toSearch moves the resource engine's LIKE filter on the search column
// into opts.Search. Anything the caller filtered by other columns stays
// a filter.
func toSearch(opts crud.ListOptions) crud.ListOptions {
	for i, f := range opts.Filters {
		if f.Op != filter.OpLike {
			continue
		}
		opts.Search = fmt.Sprint(f.Value)
		opts.Filters = append(opts.Filters[:i:i], opts.Filters[i+1:]...)
		return opts
	}
	return opts
}

// localUser is the menu-handler stand-in for the request identity: a
// value whose GetID feeds the owner extractor battery/desktop installs.
type localUser struct{ id string }

func (u localUser) GetID() string      { return u.id }
func (u localUser) GetEmail() string   { return "local@" + u.id }
func (u localUser) GetRoles() []string { return nil }

// localUserCtx returns a context carrying the desktop local identity,
// for code that runs outside a request (the File > Export handler) but
// must see the same owner-scoped rows the screens show.
func localUserCtx(ctx context.Context, d *desktop.Battery) context.Context {
	return handler.SetUser(ctx, localUser{id: d.LocalUserID()})
}
