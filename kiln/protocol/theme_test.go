package protocol_test

import (
	"context"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/gofastr/gofastr/kiln/protocol"
)

// TestSetThemeUpdatesWorldAppTheme is the protocol-level guarantee:
// SetTheme writes to world.App.Theme via a SetAppConfig journal entry,
// so replay reproduces the override exactly. The renderer reads
// world.App.Theme on each /kiln/theme.css fetch.
func TestSetThemeUpdatesWorldAppTheme(t *testing.T) {
	tools := newTools(t)

	res := tools.SetTheme(context.Background(), protocol.SetThemeArgs{
		Theme: map[string]string{
			"page-bg":      "#0F172A",
			"page-primary": "#22D3EE",
		},
	})
	if !res.OK {
		t.Fatalf("SetTheme failed: %+v", res)
	}

	app := tools.Live().Session().World.App
	if app.Theme["page-bg"] != "#0F172A" {
		t.Errorf("page-bg = %q, want #0F172A", app.Theme["page-bg"])
	}
	if app.Theme["page-primary"] != "#22D3EE" {
		t.Errorf("page-primary = %q, want #22D3EE", app.Theme["page-primary"])
	}

	// Empty Theme clears overrides.
	res = tools.SetTheme(context.Background(), protocol.SetThemeArgs{Theme: nil})
	if !res.OK {
		t.Fatalf("clear failed: %+v", res)
	}
	if got := tools.Live().Session().World.App.Theme; len(got) != 0 {
		t.Errorf("Theme not cleared: %v", got)
	}
}
