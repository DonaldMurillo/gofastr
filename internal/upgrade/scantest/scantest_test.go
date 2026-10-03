package scantest

import (
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/upgrade"
)

func TestKitStubResolvesUses(t *testing.T) {
	kit := map[string]string{"framework/ui/ui.go": "package ui\n\nfunc SiteHeader() string { return \"\" }\n"}
	app := App(t, map[string]string{
		"main.go": "package main\n\nimport \"github.com/DonaldMurillo/gofastr/framework/ui\"\n\nfunc main() { _ = ui.SiteHeader() }\n",
	}, Options{Kit: kit})
	n := &upgrade.Note{Find: upgrade.Find{Uses: []upgrade.Symbol{{Pkg: upgrade.ModulePath + "/framework/ui", Name: "SiteHeader"}}}}
	res := Run(t, app, []*upgrade.Note{n}, upgrade.MarkerSinks{})
	if !res.TypeChecked {
		t.Fatalf("app did not type-check against the stub: %v", res.Unexplained)
	}
	want := []string{"main.go:5:22 " + upgrade.ModulePath + "/framework/ui.SiteHeader"}
	if got := Hits(res, n); !slices.Equal(got, want) {
		t.Fatalf("hits = %v, want %v", got, want)
	}
}

func TestStringsNeedNoKit(t *testing.T) {
	app := App(t, map[string]string{"main.go": "package main\n\nvar c = \"ui-shell\"\n\nfunc main() { _ = c }\n"}, Options{})
	n := &upgrade.Note{Find: upgrade.Find{Strings: upgrade.StringMatch{Classes: []string{"ui-shell"}}}}
	res := Run(t, app, []*upgrade.Note{n}, upgrade.MarkerSinks{})
	if got := Hits(res, n); len(got) != 1 {
		t.Fatalf("hits = %v, want one", got)
	}
}

func TestOnlyKeepsNamedKinds(t *testing.T) {
	n := &upgrade.Note{Find: upgrade.Find{
		Uses:    []upgrade.Symbol{{Pkg: "p", Name: "N"}},
		Strings: upgrade.StringMatch{Classes: []string{"x"}},
	}}
	got := Only(n, "strings")
	if len(got.Find.Uses) != 0 || len(got.Find.Strings.Classes) != 1 || len(n.Find.Uses) != 1 {
		t.Fatalf("Only = %+v (original %+v), want strings kept, uses dropped, original intact", got.Find, n.Find)
	}
}
