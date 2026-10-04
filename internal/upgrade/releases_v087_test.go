package upgrade_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/upgrade"
	"github.com/DonaldMurillo/gofastr/internal/upgrade/scantest"
)

// The pending v0.87.0 note, driven through the shipped YAML and the real
// scan engine against a stub kit: every WithMCPIntrospection call is a
// review hit (the app decides whether to add framework.WithMCPTools(mcptools.Register)),
// and an app that never asked for introspection is silent.
func TestV087DocsToolsNoteHitsIntrospectionCalls(t *testing.T) {
	reg, err := upgrade.Load()
	if err != nil {
		t.Fatalf("upgrade.Load: %v", err)
	}
	n := scantest.Only(scantest.Note(t, reg, "v0.87.0", 0), "uses")
	if !n.Review {
		t.Fatalf("v0.87.0/0 must be review-tier: each hit is a decision, not an edit")
	}
	kit := map[string]string{"framework/app.go": `package framework

type App struct{}
type AppOption func(*App)

func WithMCP() AppOption              { return func(*App) {} }
func WithMCPIntrospection() AppOption { return func(*App) {} }
func NewApp(opts ...AppOption) *App   { return &App{} }
`}
	app := scantest.App(t, map[string]string{"main.go": `package main

import "github.com/DonaldMurillo/gofastr/framework"

func main() {
	_ = framework.NewApp(framework.WithMCP(), framework.WithMCPIntrospection())
}
`}, scantest.Options{Kit: kit})
	res := scantest.Run(t, app, []*upgrade.Note{n}, upgrade.MarkerSinks{})
	if !res.TypeChecked {
		t.Fatalf("app did not type-check: broken=%v unexplained=%v", res.Broken, res.Unexplained)
	}
	got := scantest.Hits(res, n)
	if len(got) != 1 || !strings.Contains(got[0], "WithMCPIntrospection") {
		t.Fatalf("hits = %v, want the one WithMCPIntrospection call", got)
	}

	quiet := scantest.App(t, map[string]string{"main.go": `package main

import "github.com/DonaldMurillo/gofastr/framework"

func main() { _ = framework.NewApp(framework.WithMCP()) }
`}, scantest.Options{Kit: kit})
	if got := scantest.Hits(scantest.Run(t, quiet, []*upgrade.Note{n}, upgrade.MarkerSinks{}), n); len(got) != 0 {
		t.Fatalf("fires with no introspection call: %v", got)
	}
}
