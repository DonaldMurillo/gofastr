package journal_test

import (
	"errors"
	"testing"

	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// set_theme journals the whole app config it read. A config edit that
// landed after that read must not be overwritten by it.
func TestStaleAppConfigIsConflict(t *testing.T) {
	s := journal.NewSession()
	base := world.AppConfig{Name: "shop", APIPrefix: "api"}
	renamed := world.AppConfig{Name: "store", APIPrefix: "api"}
	themed := base
	themed.Theme = map[string]string{"primary": "#123456"}
	entries := []journal.Entry{
		pageEntry(t, "1", journal.OpSetAppConfig, journal.SetAppConfigPayload{Config: base}),
		pageEntry(t, "2", journal.OpSetAppConfig, journal.SetAppConfigPayload{Config: renamed, Prev: &base}),
		pageEntry(t, "3", journal.OpSetAppConfig, journal.SetAppConfigPayload{Config: themed, Prev: &base}),
	}
	var err error
	for _, e := range entries {
		if err = journal.Apply(s, e); err != nil {
			break
		}
	}
	if !errors.Is(err, journal.ErrAppConfigChanged) {
		t.Fatalf("err = %v, want ErrAppConfigChanged", err)
	}
	if s.World.App.Name != "store" {
		t.Fatalf("name = %q, want the rename kept", s.World.App.Name)
	}

	// A log that already holds the race loads as it ran.
	r, err := journal.ReplayEntries(entries)
	if err != nil {
		t.Fatal(err)
	}
	if r.World.App.Theme["primary"] != "#123456" {
		t.Fatalf("replayed theme = %v, want the later entry applied", r.World.App.Theme)
	}
}

// An empty theme map read back from JSON is nil: Prev compares by its
// JSON form, so the round trip is no conflict.
func TestAppConfigPrevComparesAsJSON(t *testing.T) {
	s := journal.NewSession()
	s.World.App = world.AppConfig{Name: "shop", APIPrefix: "api", Theme: map[string]string{}}
	prev := world.AppConfig{Name: "shop", APIPrefix: "api"}
	next := world.AppConfig{Name: "shop", APIPrefix: "api", Theme: map[string]string{"primary": "#000"}}
	if err := journal.Apply(s, pageEntry(t, "1", journal.OpSetAppConfig, journal.SetAppConfigPayload{Config: next, Prev: &prev})); err != nil {
		t.Fatal(err)
	}
}
