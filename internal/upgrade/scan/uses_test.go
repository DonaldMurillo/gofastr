package scan

import (
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

import "example.com/kit/ui"

func TestHeader(t *testing.T) {
	_ = ui.SiteHeader("t")
}
`
	n := &upgrade.Note{Find: upgrade.Find{Uses: []upgrade.Symbol{siteHeaderSym}}}
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{
		"main.go":      "package main\n\nfunc main() {}\n",
		"main_test.go": src,
	}), n)
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
