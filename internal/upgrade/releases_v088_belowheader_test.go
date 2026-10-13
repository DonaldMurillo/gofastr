package upgrade_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/upgrade"
	"github.com/DonaldMurillo/gofastr/internal/upgrade/scantest"
)

// belowHeaderKit is the v0.87 ui surface the BelowHeader note matches,
// with the StackConfig.StickyHeader that replaced it.
var belowHeaderKit = map[string]string{"framework/ui/ui.go": `package ui

type HTML string
type ContentRowConfig struct {
	Sticky      bool
	BelowHeader bool
}
type StackConfig struct {
	Screen       bool
	StickyHeader bool
}

func ContentRow(cfg ContentRowConfig, main ...HTML) HTML { return "" }
func Stack(cfg StackConfig, children ...HTML) HTML      { return "" }
`}

func belowHeaderNote(t *testing.T) *upgrade.Note {
	t.Helper()
	reg, err := upgrade.Load()
	if err != nil {
		t.Fatalf("upgrade.Load: %v", err)
	}
	for _, rel := range reg.Releases {
		if rel.Version != "v0.88.0" {
			continue
		}
		for i := range rel.Notes {
			if strings.HasPrefix(rel.Notes[i].Change, "ui.ContentRowConfig.BelowHeader is removed") {
				return rel.Notes[i]
			}
		}
	}
	t.Fatal("no v0.88.0 note for the removed ContentRow BelowHeader")
	return nil
}

// A row that sets BelowHeader is found; the frame the guidance moves it
// to (StickyHeader on the page stack) is quiet.
func TestV088BelowHeaderNoteHitsField(t *testing.T) {
	n := belowHeaderNote(t)
	old := scantest.App(t, map[string]string{"main.go": `package main

import "github.com/DonaldMurillo/gofastr/framework/ui"

func main() {
	_ = ui.ContentRow(ui.ContentRowConfig{Sticky: true, BelowHeader: true})
}
`}, scantest.Options{Kit: belowHeaderKit})
	if got := scantest.Hits(scantest.Run(t, old, []*upgrade.Note{n}, upgrade.MarkerSinks{}), n); len(got) != 1 {
		t.Fatalf("hits = %v, want the BelowHeader key", got)
	}
	fixed := scantest.App(t, map[string]string{"main.go": `package main

import "github.com/DonaldMurillo/gofastr/framework/ui"

func main() {
	_ = ui.Stack(ui.StackConfig{Screen: true, StickyHeader: true},
		ui.ContentRow(ui.ContentRowConfig{Sticky: true}))
}
`}, scantest.Options{Kit: belowHeaderKit})
	if got := scantest.Hits(scantest.Run(t, fixed, []*upgrade.Note{n}, upgrade.MarkerSinks{}), n); len(got) != 0 {
		t.Fatalf("fires on the StickyHeader frame: %v", got)
	}
}
