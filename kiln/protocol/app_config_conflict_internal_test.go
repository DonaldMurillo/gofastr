package protocol

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/live"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// set_theme built from a config a rename already replaced answers
// "conflict" and keeps the rename.
func TestStaleThemeAnswersConflict(t *testing.T) {
	l, err := live.New(journal.NewMemory(), func() *framework.App { return framework.NewApp() })
	if err != nil {
		t.Fatal(err)
	}
	tools := New(l)
	ctx := context.Background()
	if r := tools.SetAppConfig(ctx, SetAppConfigArgs{Config: world.AppConfig{Name: "shop"}}); !r.OK {
		t.Fatalf("SetAppConfig: %+v", r)
	}
	var read world.AppConfig
	l.ReadSession(func(s *journal.Session) { read = s.World.App })
	if r := tools.SetAppConfig(ctx, SetAppConfigArgs{Config: world.AppConfig{Name: "store"}}); !r.OK {
		t.Fatalf("rename: %+v", r)
	}
	themed := read
	themed.Theme = map[string]string{"primary": "#123456"}
	r := tools.applyEdit(journal.OpSetAppConfig, journal.SetAppConfigPayload{Config: themed, Prev: &read})
	if r.OK || r.Kind != "conflict" {
		t.Fatalf("stale theme = %+v, want conflict", r)
	}
	var name string
	l.ReadSession(func(s *journal.Session) { name = s.World.App.Name })
	if name != "store" {
		t.Fatalf("name = %q, want the rename kept", name)
	}
	if r := tools.SetTheme(ctx, SetThemeArgs{Theme: map[string]string{"primary": "#123456"}}); !r.OK {
		t.Fatalf("fresh SetTheme: %+v", r)
	}
}
