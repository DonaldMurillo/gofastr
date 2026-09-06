package main

import (
	"context"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
)

// The three entities. All owner-scoped (hard rule 6): every row belongs
// to the local identity (or, under --serve with a harness user, that
// user), and the same entities run in the desktop window and behind
// --serve. The timer's state is the sessions table; no server RAM.

// registerTasksEntity declares the work items. estimate is pomodoros
// the task is expected to take; completed_pomodoros counts finished
// work sessions.
func registerTasksEntity(app *framework.App) {
	// The desktop battery installs the local identity middleware (its Init); owner-scoped CRUD answers the window's own requests without battery/auth
	app.Entity("tasks", framework.EntityConfig{
		Scope:        &framework.ScopeConfig{OwnerField: "user_id"},
		SearchFields: []string{"title"},
		Fields: []schema.Field{
			{Name: "user_id", Type: schema.String},
			{Name: "title", Type: schema.String, Required: true, Max: new(120.0)},
			{Name: "note", Type: schema.Text},
			{Name: "done", Type: schema.Bool, Default: false},
			{Name: "estimate", Type: schema.Int, Default: 1},
			{Name: "completed_pomodoros", Type: schema.Int, Default: 0},
		},
	})
}

// registerSessionsEntity declares the timer's log and state. The
// newest row with completed == false IS the current session; started_at,
// ends_at, and paused_at are data (RFC 3339 strings), while created_at
// and updated_at come from the entity's Timestamps default and are
// never declared by hand.
func registerSessionsEntity(app *framework.App) {
	// The desktop battery installs the local identity middleware (its Init); owner-scoped CRUD answers the window's own requests without battery/auth
	app.Entity("sessions", framework.EntityConfig{
		Scope: &framework.ScopeConfig{OwnerField: "user_id"},
		Fields: []schema.Field{
			{Name: "user_id", Type: schema.String},
			{Name: "task_id", Type: schema.String},
			{Name: "kind", Type: schema.Enum, Values: []string{"work", "break"}, Default: "work"},
			{Name: "started_at", Type: schema.Timestamp},
			{Name: "ends_at", Type: schema.Timestamp},
			{Name: "paused_at", Type: schema.Timestamp},
			{Name: "paused_left", Type: schema.Int, Default: 0},
			{Name: "completed", Type: schema.Bool, Default: false},
			{Name: "minutes", Type: schema.Int, Default: 0},
		},
	})
}

// registerSettingsEntity declares the per-owner settings row: one row
// per local user, created on the first /settings visit.
func registerSettingsEntity(app *framework.App) {
	// The desktop battery installs the local identity middleware (its Init); owner-scoped CRUD answers the window's own requests without battery/auth
	app.Entity("settings", framework.EntityConfig{
		Scope: &framework.ScopeConfig{OwnerField: "user_id"},
		Fields: []schema.Field{
			{Name: "user_id", Type: schema.String},
			{Name: "work_minutes", Type: schema.Int, Default: defaultWorkMinutes},
			{Name: "break_minutes", Type: schema.Int, Default: defaultBreakMinutes},
			{Name: "notify", Type: schema.Bool, Default: true},
			{Name: "tray_countdown", Type: schema.Bool, Default: true},
		},
	})
}

// Settings defaults, shared by the entity declaration, the settings
// screen's first-visit row creation, and the engine's no-row fallback.
const (
	defaultWorkMinutes  = 25
	defaultBreakMinutes = 5
)

// focusSettings is the owner's preferences as the engine reads them.
type focusSettings struct {
	WorkMinutes   int
	BreakMinutes  int
	Notify        bool
	TrayCountdown bool
}

// defaultFocusSettings is the fallback when the owner has no row yet.
func defaultFocusSettings() focusSettings {
	return focusSettings{
		WorkMinutes:   defaultWorkMinutes,
		BreakMinutes:  defaultBreakMinutes,
		Notify:        true,
		TrayCountdown: true,
	}
}

// localUser is the out-of-request stand-in for the request identity: a
// value whose GetID feeds the owner extractor battery/desktop installs
// (menu handlers, the tick loop, the deep-link handler).
type localUser struct{ id string }

func (u localUser) GetID() string      { return u.id }
func (u localUser) GetEmail() string   { return "local@" + u.id }
func (u localUser) GetRoles() []string { return nil }

// localUserCtx returns a context carrying the desktop local identity,
// for code that runs outside a request (the tick loop, menu handlers)
// but must see the same owner-scoped rows the screens show.
func localUserCtx(ctx context.Context, d *desktop.Battery) context.Context {
	return handler.SetUser(ctx, localUser{id: d.LocalUserID()})
}
