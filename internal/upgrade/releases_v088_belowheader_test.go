package upgrade_test

import "testing"

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

// A row that sets BelowHeader is found; the frame the guidance moves it
// to (StickyHeader on the page stack) is quiet.
func TestV088BelowHeaderNoteHitsField(t *testing.T) {
	n := v088Note(t, "ui.ContentRowConfig.BelowHeader is removed")
	if got := v088Hits(t, n, belowHeaderKit, `package main

import "github.com/DonaldMurillo/gofastr/framework/ui"

func main() {
	_ = ui.ContentRow(ui.ContentRowConfig{Sticky: true, BelowHeader: true})
}
`); len(got) != 1 {
		t.Fatalf("hits = %v, want the BelowHeader key", got)
	}
	if got := v088Hits(t, n, belowHeaderKit, `package main

import "github.com/DonaldMurillo/gofastr/framework/ui"

func main() {
	_ = ui.Stack(ui.StackConfig{Screen: true, StickyHeader: true},
		ui.ContentRow(ui.ContentRowConfig{Sticky: true}))
}
`); len(got) != 0 {
		t.Fatalf("fires on the StickyHeader frame: %v", got)
	}
}
