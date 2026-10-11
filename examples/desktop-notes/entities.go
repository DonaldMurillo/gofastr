package main

import (
	"context"
	"errors"
	"log/slog"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
)

// The notes entity and its hooks. Everything here is plain GoFastr: no
// desktop-specific code except the AfterCreate notification, which is
// the point of the example. The same entity, hooks, and screens run in
// the desktop window and behind `--serve`.

// registerNotesEntity declares the one entity. Owner-scoped (hard rule
// 6): every row belongs to the local identity, and SearchFields puts
// both text columns behind ?q=. Display is what the screens read: the
// title names a record and the list shows it beside the last edit.
// user_id is the owner stamp — the API returns it, no screen shows it.
func registerNotesEntity(app *framework.App) {
	// the desktop battery installs the local identity middleware (its Init); owner-scoped CRUD answers the window's own requests without battery/auth
	app.Entity("notes", framework.EntityConfig{
		Scope:        &framework.ScopeConfig{OwnerField: "user_id"},
		SearchFields: []string{"title", "body"},
		Display: &framework.DisplayConfig{
			Singular:   "Note",
			Plural:     "Notes",
			TitleField: "title",
			Columns:    []string{"title", "updated_at"},
			Fields: map[string]framework.FieldDisplay{
				"user_id": {Omit: true},
			},
		},
		Fields: []schema.Field{
			{Name: "user_id", Type: schema.String},
			{Name: "title", Type: schema.String, Required: true, Max: new(200.0)},
			{Name: "body", Type: schema.Text},
		},
	})
}

// The app's settings are not an entity: notify_on_save and
// export_folder are preferences declared on desktop.Config (see
// buildApp), stored in the app state under "settings".

// noteRow is the typed hook payload. The framework hands hooks a
// snake_cased map; the typed wrappers translate through these json
// tags, so camelCase tags are the contract.
type noteRow struct {
	Title string `json:"title"`
}

// registerNotesHooks fires a native notification after a save.
// created_at and updated_at come from the entity's default
// Timestamps=true and are auto-stamped. ErrUnsupported is the
// unbundled run (`go run` outside a signed .app, or a host without a
// native shell): the notification is skipped, not an error. Anything
// else is logged at Warn; a failed toast must never fail the save.
func registerNotesHooks(app *framework.App, d *desktop.Battery) {
	framework.OnAfterCreate[noteRow](app, "notes", func(ctx context.Context, n *noteRow) error {
		if d == nil {
			return nil
		}
		if !notifyOnSave(d) {
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

// notifyOnSave reads the notify_on_save preference (default true).
// Before Run opens the app state store (the --serve shape) that is
// the default, matching the notification's history of never failing
// the save.
func notifyOnSave(d *desktop.Battery) bool {
	if d == nil {
		return true
	}
	return d.Preferences().Bool("notify_on_save")
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
