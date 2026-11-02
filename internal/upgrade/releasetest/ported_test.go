package releasetest

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/upgrade"
	"github.com/DonaldMurillo/gofastr/internal/upgrade/scantest"
)

// A hits: edit note says every hit spells something its release no
// longer accepts, so an app that finished the port must scan clean.
// These pin that for notes whose symbol survives the release with a new
// shape: the old spelling is found, the ported one is not.

// shippedNote is the note of version whose change starts with prefix,
// read through the shipped YAML.
func shippedNote(t *testing.T, version, prefix string) *upgrade.Note {
	t.Helper()
	for _, rel := range scantest.Registry(t).Releases {
		if rel.Version != version {
			continue
		}
		for _, n := range rel.Notes {
			if strings.HasPrefix(n.Change, prefix) {
				return n
			}
		}
	}
	t.Fatalf("no %s note starting %q", version, prefix)
	return nil
}

func noteHits(t *testing.T, n *upgrade.Note, kit map[string]string, mainGo string) []string {
	t.Helper()
	app := scantest.App(t, map[string]string{"main.go": mainGo}, scantest.Options{Kit: kit})
	return scantest.Hits(scantest.Run(t, app, []*upgrade.Note{n}, upgrade.MarkerSinks{}), n)
}

// The v0.96 llm.md helpers took the screen alone; the ported call
// passes a context first and must not be reported as an edit.
func TestV096ScreenLLMMDNoteSilentOncePorted(t *testing.T) {
	n := shippedNote(t, "v0.96.0", "BREAKING - app.ScreenLLMMD and app.ScreenLLMMDWithMeta take a context.Context first")
	if n.Review {
		t.Fatal("the note must be hits: edit")
	}
	oldKit := map[string]string{"core-ui/app/app.go": `package app

type Screen struct{}

func ScreenLLMMD(screen *Screen) string                         { return "" }
func ScreenLLMMDWithMeta(screen *Screen, metaPrefix string) string { return "" }
`}
	if got := noteHits(t, n, oldKit, `package main

import "github.com/DonaldMurillo/gofastr/core-ui/app"

func main() {
	_ = app.ScreenLLMMD(&app.Screen{})
	_ = app.ScreenLLMMDWithMeta(&app.Screen{}, "x")
}
`); len(got) != 2 {
		t.Fatalf("hits = %v, want both old calls", got)
	}
	newKit := map[string]string{"core-ui/app/app.go": `package app

import "context"

type Screen struct{}

func ScreenLLMMD(ctx context.Context, screen *Screen) string                         { return "" }
func ScreenLLMMDWithMeta(ctx context.Context, screen *Screen, metaPrefix string) string { return "" }
`}
	if got := noteHits(t, n, newKit, `package main

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
)

func main() {
	ctx := context.Background()
	_ = app.ScreenLLMMD(ctx, &app.Screen{})
	_ = app.ScreenLLMMDWithMeta(ctx, &app.Screen{}, "x")
}
`); len(got) != 0 {
		t.Fatalf("the ported calls are reported as edits: %v", got)
	}
}

// RailWidth became a string type of named widths. An old CSS length
// still compiles against it and panics at render, so the note finds a
// length whether the app builds against the old kit or the new one, and
// stays quiet on the named widths.
func TestV087RailWidthNoteHitsLengthsOnly(t *testing.T) {
	n := shippedNote(t, "v0.87.0", "ui.WorkbenchConfig.RailWidth is a ui.WorkbenchRailWidth")
	oldKit := map[string]string{"framework/ui/ui.go": `package ui

type WorkbenchConfig struct{ RailWidth string }
`}
	newKit := map[string]string{"framework/ui/ui.go": `package ui

type WorkbenchRailWidth string

const (
	WorkbenchRailDefault WorkbenchRailWidth = ""
	WorkbenchRailNarrow  WorkbenchRailWidth = "narrow"
	WorkbenchRailWide    WorkbenchRailWidth = "wide"
)

type WorkbenchConfig struct{ RailWidth WorkbenchRailWidth }
`}
	const lengths = `package main

import "github.com/DonaldMurillo/gofastr/framework/ui"

func main() {
	_ = ui.WorkbenchConfig{RailWidth: "300px"}
	_ = ui.WorkbenchConfig{RailWidth: "20rem"}
	_ = ui.WorkbenchConfig{RailWidth: "na"}
	_ = ui.WorkbenchConfig{RailWidth: "wider"}
}
`
	for name, kit := range map[string]map[string]string{"old kit": oldKit, "new kit": newKit} {
		if got := noteHits(t, n, kit, lengths); len(got) != 4 {
			t.Errorf("%s: hits = %v, want the four lengths", name, got)
		}
	}
	if got := noteHits(t, n, newKit, `package main

import "github.com/DonaldMurillo/gofastr/framework/ui"

func main() {
	_ = ui.WorkbenchConfig{RailWidth: ui.WorkbenchRailWide}
	_ = ui.WorkbenchConfig{RailWidth: ui.WorkbenchRailNarrow}
	_ = ui.WorkbenchConfig{RailWidth: "wide"}
	_ = ui.WorkbenchConfig{}
}
`); len(got) != 0 {
		t.Fatalf("the named widths are reported: %v", got)
	}
}
