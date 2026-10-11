package main

// Pins against the app's own entityui value (appUI) what the old
// resource-registry tests pinned against appResources: a metric or chart
// over an entity that does not exist fails closed (an em dash, empty
// bars), and the owner-scoped list draws its refusal, never rows, for a
// caller with no user on the context.

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/entityui"

	"github.com/DonaldMurillo/gofastr/examples/meridian/entities"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// newTestAppUI builds the app's UI over the real registry, the way
// RegisterGenerated does. No migration runs: both pins answer before any
// query would (the entity lookup, the read gate).
func newTestAppUI(t *testing.T) *entityui.UI {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	fwApp := framework.NewApp(framework.WithDB(db))
	entities.RegisterAll(fwApp)
	return fwApp.EntityUI(appExtensions)
}

func TestMissingEntityStatFailsClosed(t *testing.T) {
	ui := newTestAppUI(t)
	if got := ui.StatValue(context.Background(), "missing", "count", "", "", ""); got != "—" {
		t.Fatalf("StatValue for missing entity = %q, want em dash", got)
	}
	if bars := ui.GroupBars(context.Background(), "missing", "status"); len(bars) != 0 {
		t.Fatalf("GroupBars for missing entity = %#v, want empty", bars)
	}
}

func TestAnonymousListDrawsRefusalNotRows(t *testing.T) {
	ui := newTestAppUI(t)
	html := ui.List("customers").RenderCtx(context.Background()).String()
	if !strings.Contains(html, entityui.AccessDeniedTitle) {
		t.Fatalf("anonymous list on the owner-scoped customers entity must draw the not-available notice, got:\n%s", html)
	}
	if strings.Contains(html, "<table") {
		t.Fatalf("anonymous list rendered a table; the read gate must refuse before any row is read:\n%s", html)
	}
}
