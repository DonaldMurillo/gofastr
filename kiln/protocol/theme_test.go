package protocol_test

import (
	"context"
	"testing"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"

	"github.com/DonaldMurillo/gofastr/kiln/protocol"
)

// TestSetThemeUpdatesWorldAppTheme is the protocol-level guarantee:
// SetTheme writes to world.App.Theme via a SetAppConfig journal entry,
// so replay reproduces the override exactly. The renderer reads
// world.App.Theme while replay reproduces the semantic tokens exactly.
func TestSetThemeUpdatesWorldAppTheme(t *testing.T) {
	tools := newTools(t)

	res := tools.SetTheme(context.Background(), protocol.SetThemeArgs{
		Theme: map[string]string{
			"background": "#0F172A",
			"primary":    "#22D3EE",
			// A light cyan primary needs a dark ink: Theme.Validate refuses a
			// primary pair under 4.5:1 (white on this cyan is 1.81:1).
			"primary-fg": "#0B1220",
		},
	})
	if !res.OK {
		t.Fatalf("SetTheme failed: %+v", res)
	}

	app := tools.Live().Session().World.App
	if app.Theme["background"] != "#0F172A" {
		t.Errorf("background = %q, want #0F172A", app.Theme["background"])
	}
	if app.Theme["primary"] != "#22D3EE" {
		t.Errorf("primary = %q, want #22D3EE", app.Theme["primary"])
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
