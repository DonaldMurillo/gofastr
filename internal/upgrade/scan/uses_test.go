package scan

import (
	"maps"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/upgrade"
)

var siteHeaderSym = upgrade.Symbol{Pkg: "example.com/kit/ui", Name: "SiteHeader"}
var withHeaderSym = upgrade.Symbol{Pkg: "example.com/kit/ui", Name: "Layout", Member: "WithHeader"}

func TestUsesCall(t *testing.T) {
	src := `package main

import "example.com/kit/ui"

func main() {
	_ = ui.SiteHeader("t")
}
`
	n := &upgrade.Note{Find: upgrade.Find{Uses: []upgrade.Symbol{siteHeaderSym}}}
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{"main.go": src}), n)
	wantHits(t, res, n, hitAt(src, "SiteHeader", "main.go", siteHeaderSym.String()))
}

func TestUsesMethodValue(t *testing.T) {
	src := `package main

import "example.com/kit/ui"

func main() {
	var l ui.Layout
	f := l.WithHeader
	f("h")
}
`
	n := &upgrade.Note{Find: upgrade.Find{Uses: []upgrade.Symbol{withHeaderSym}}}
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{"main.go": src}), n)
	wantHits(t, res, n, hitAt(src, "WithHeader", "main.go", withHeaderSym.String()))
}

func TestUsesCallSplitLines(t *testing.T) {
	src := `package main

import "example.com/kit/ui"

func main() {
	_ = ui.
		SiteHeader(
			"t",
		)
}
`
	n := &upgrade.Note{Find: upgrade.Find{Uses: []upgrade.Symbol{siteHeaderSym}}}
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{"main.go": src}), n)
	wantHits(t, res, n, hitAt(src, "SiteHeader", "main.go", siteHeaderSym.String()))
}

func TestUsesImportAlias(t *testing.T) {
	src := `package main

import kitui "example.com/kit/ui"

func main() {
	_ = kitui.SiteHeader("t")
}
`
	n := &upgrade.Note{Find: upgrade.Find{Uses: []upgrade.Symbol{siteHeaderSym}}}
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{"main.go": src}), n)
	wantHits(t, res, n, hitAt(src, "SiteHeader", "main.go", siteHeaderSym.String()))
}

func TestUsesPromotedField(t *testing.T) {
	src := `package main

import "example.com/kit/ui"

func main() {
	var w ui.Wrapper
	_ = w.Promoted
}
`
	// The symbol names the DECLARING type (Embedded), and the hit reports
	// that symbol even though the app spells the embedding type (Wrapper).
	sym := upgrade.Symbol{Pkg: "example.com/kit/ui", Name: "Embedded", Member: "Promoted"}
	n := &upgrade.Note{Find: upgrade.Find{Uses: []upgrade.Symbol{sym}}}
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{"main.go": src}), n)
	wantHits(t, res, n, hitAt(src, "Promoted", "main.go", sym.String()))
}

func TestUsesCompositeKey(t *testing.T) {
	src := `package main

import "example.com/kit/ui"

func main() {
	_ = ui.ButtonConfig{
		ExtraAttrs: ui.Attrs{},
	}
}
`
	sym := upgrade.Symbol{Pkg: "example.com/kit/ui", Name: "ButtonConfig", Member: "ExtraAttrs"}
	n := &upgrade.Note{Find: upgrade.Find{Uses: []upgrade.Symbol{sym}}}
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{"main.go": src}), n)
	wantHits(t, res, n, hitAt(src, "ExtraAttrs", "main.go", sym.String()))
}

func TestUsesGenericInstantiation(t *testing.T) {
	src := `package main

import "example.com/kit/ui"

func main() {
	st := ui.NewStack[string]()
	st.Items = append(st.Items, "x")
}
`
	sym := upgrade.Symbol{Pkg: "example.com/kit/ui", Name: "NewStack"}
	n := &upgrade.Note{Find: upgrade.Find{Uses: []upgrade.Symbol{sym}}}
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{"main.go": src}), n)
	wantHits(t, res, n, hitAt(src, "NewStack", "main.go", sym.String()))
}

func TestUsesInTestFile(t *testing.T) {
	src := `package main

import (
	"testing"

	"example.com/kit/ui"
)

func TestHeader(t *testing.T) {
	_ = ui.SiteHeader("t")
}
`
	n := &upgrade.Note{Find: upgrade.Find{Uses: []upgrade.Symbol{siteHeaderSym}}}
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{
		"main.go":      "package main\n\nfunc main() {}\n",
		"main_test.go": src,
	}), n)
	// The typed matcher must find it, not the compile-error fallback.
	if !res.TypeChecked {
		t.Fatalf("test variant did not type-check: %v", res.Broken)
	}
	wantHits(t, res, n, hitAt(src, "SiteHeader", "main_test.go", siteHeaderSym.String()))
}

func TestUsesIgnoresSameNameMethod(t *testing.T) {
	src := `package main

type mailer struct{}

func (mailer) WithHeader(k, v string) {}

func main() {
	m := mailer{}
	m.WithHeader("X", "v")
}
`
	n := &upgrade.Note{Find: upgrade.Find{Uses: []upgrade.Symbol{withHeaderSym}}}
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{"main.go": src}), n)
	wantHits(t, res, n)
}

func TestUsesIgnoresLocalVar(t *testing.T) {
	src := `package main

func main() {
	SiteHeader := func() string { return "" }
	_ = SiteHeader()
}
`
	n := &upgrade.Note{Find: upgrade.Find{Uses: []upgrade.Symbol{siteHeaderSym}}}
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{"main.go": src}), n)
	wantHits(t, res, n)
}

var queueAckSym = upgrade.Symbol{Pkg: "example.com/kit/queue", Name: "Queue", Member: "Ack"}
var queueNackSym = upgrade.Symbol{Pkg: "example.com/kit/queue", Name: "Queue", Member: "Nack"}

func TestUsesInterfaceImplMethods(t *testing.T) {
	src := `package main

import "example.com/kit/queue"

type myQueue struct{}

func (myQueue) Ack() error  { return nil }
func (myQueue) Nack() error { return nil }

var _ queue.Queue = myQueue{}

func main() {}
`
	// Declaring the methods IS the use: custom implementations change
	// with the interface, call sites or not.
	n := &upgrade.Note{Find: upgrade.Find{Uses: []upgrade.Symbol{queueAckSym, queueNackSym}}}
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{"main.go": src}), n)
	wantHits(t, res, n,
		hitAt(src, "Ack() error", "main.go", queueAckSym.String()),
		hitAt(src, "Nack() error", "main.go", queueNackSym.String()))
}

func TestUsesInterfaceImplOtherTypeSilent(t *testing.T) {
	src := `package main

import "example.com/kit/queue"

type myQueue struct{}
type unrelated struct{}

func (myQueue) Ack() error  { return nil }
func (myQueue) Nack() error { return nil }
func (unrelated) Ack() error { return nil }

var _ queue.Queue = myQueue{}

func main() {}
`
	// A same-named method on a type that does not implement the
	// interface is not a use of the interface's method.
	n := &upgrade.Note{Find: upgrade.Find{Uses: []upgrade.Symbol{queueAckSym, queueNackSym}}}
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{"main.go": src}), n)
	wantHits(t, res, n,
		hitAt(src, "Ack() error", "main.go", queueAckSym.String()),
		hitAt(src, "Nack() error", "main.go", queueNackSym.String()))
}

func TestUsesAliasTypeLiteral(t *testing.T) {
	src := `package main

import "example.com/kit/fw"

func main() {
	l := fw.Layout{Title: "t"}
	l.WithHeader("h")
}
`
	// The app spells the re-export alias; the note names the type that
	// declares it.
	sym := upgrade.Symbol{Pkg: "example.com/kit/ui", Name: "Layout"}
	n := &upgrade.Note{Find: upgrade.Find{Uses: []upgrade.Symbol{sym}}}
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{"main.go": src}), n)
	wantHits(t, res, n, hitAt(src, "Layout{", "main.go", sym.String()))
}

func TestUsesAliasFieldAccess(t *testing.T) {
	src := `package main

import "example.com/kit/fw"

func main() {
	var l fw.Layout
	_ = l.Title
}
`
	sym := upgrade.Symbol{Pkg: "example.com/kit/ui", Name: "Layout", Member: "Title"}
	n := &upgrade.Note{Find: upgrade.Find{Uses: []upgrade.Symbol{sym}}}
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{"main.go": src}), n)
	wantHits(t, res, n, hitAt(src, "Title", "main.go", sym.String()))
}

func TestUsesAliasTypeAssertion(t *testing.T) {
	src := `package main

import "example.com/kit/fw"

func main() {
	var v any
	_ = v.(fw.Layout)
}
`
	sym := upgrade.Symbol{Pkg: "example.com/kit/ui", Name: "Layout"}
	n := &upgrade.Note{Find: upgrade.Find{Uses: []upgrade.Symbol{sym}}}
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{"main.go": src}), n)
	wantHits(t, res, n, hitAt(src, "Layout)", "main.go", sym.String()))
}

func TestUsesPkgUnderBuildDir(t *testing.T) {
	src := `package build

import "example.com/kit/ui"

func Render() string { return ui.SiteHeader("t") }
`
	main := `package main

import "example.com/app/internal/build"

func main() { _ = build.Render() }
`
	// A directory named build holds Go the go tool loads; only the
	// CSS/text walk skips it.
	n := &upgrade.Note{Find: upgrade.Find{Uses: []upgrade.Symbol{siteHeaderSym}}}
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{
		"internal/build/build.go": src,
		"main.go":                 main,
	}), n)
	if !res.TypeChecked {
		t.Fatalf("fixture did not type-check: %v", res.Broken)
	}
	wantHits(t, res, n, hitAt(src, "SiteHeader", "internal/build/build.go", siteHeaderSym.String()))
}

func TestUsesAliasReceiverSelection(t *testing.T) {
	src := `package main

import "example.com/kit/fw"

func main() {
	var l fw.Layout
	l.WithHeader("h")
}
`
	// A selection on an alias-typed receiver already keys on the
	// declaring type; this pins that.
	n := &upgrade.Note{Find: upgrade.Find{Uses: []upgrade.Symbol{withHeaderSym}}}
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{"main.go": src}), n)
	wantHits(t, res, n, hitAt(src, "WithHeader", "main.go", withHeaderSym.String()))
}

func TestUsesInterfaceImplNoImport(t *testing.T) {
	impl := `package store

type Q struct{}

func (Q) Ack() error  { return nil }
func (Q) Nack() error { return nil }
`
	wire := `package main

import (
	"example.com/app/store"
	"example.com/kit/queue"
)

var _ queue.Queue = store.Q{}

func main() {}
`
	// Go interfaces are structural: the implementing package need not
	// import the interface's package, and the first package scanned may
	// not import it either.
	n := &upgrade.Note{Find: upgrade.Find{Uses: []upgrade.Symbol{queueAckSym}}}
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{
		"main.go":        wire,
		"store/store.go": impl,
	}), n)
	wantHits(t, res, n, hitAt(impl, "Ack() error", "store/store.go", queueAckSym.String()))
}

// TestUsesInterfaceImplAcrossLoads proves the interface resolves in the
// load the implementing package came from. win.go makes a windows
// configuration load re-type-check qctx and context; that load's
// qctx.Store names a different context.Context, so no type from the
// main load implements it.
func TestUsesInterfaceImplAcrossLoads(t *testing.T) {
	kit := maps.Clone(defaultKit)
	kit["qctx/qctx.go"] = "package qctx\n\nimport \"context\"\n\ntype Store interface{ Finish(ctx context.Context) error }\n"
	impl := "package store\n\nimport \"context\"\n\ntype S struct{}\n\nfunc (S) Finish(ctx context.Context) error { return nil }\n"
	wire := "package main\n\nimport (\n\t\"example.com/app/store\"\n\t\"example.com/kit/qctx\"\n)\n\nvar _ qctx.Store = store.S{}\n\nfunc main() {}\n"
	win := "//go:build windows\n\npackage main\n\nimport \"example.com/kit/qctx\"\n\nvar _ qctx.Store\n"
	sym := upgrade.Symbol{Pkg: "example.com/kit/qctx", Name: "Store", Member: "Finish"}
	n := &upgrade.Note{Find: upgrade.Find{Uses: []upgrade.Symbol{sym}}}
	res := mustRun(t, newWorkspace(t, kit, map[string]string{
		"main.go":        wire,
		"win.go":         win,
		"store/store.go": impl,
	}), n)
	wantHits(t, res, n, hitAt(impl, "Finish(ctx", "store/store.go", sym.String()))
}
