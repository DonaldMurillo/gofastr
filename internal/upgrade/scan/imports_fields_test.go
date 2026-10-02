package scan

import (
	"regexp"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/upgrade"
)

func TestImportsExact(t *testing.T) {
	src := `package main

import (
	_ "example.com/kit/patterns/accordion"
	_ "example.com/kit/patterns/treeview"
)

func main() {}
`
	n := &upgrade.Note{Find: upgrade.Find{Imports: []string{"example.com/kit/patterns/accordion"}}}
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{"main.go": src}), n)
	wantHits(t, res, n,
		hitAt(src, `"example.com/kit/patterns/accordion"`, "main.go", "import example.com/kit/patterns/accordion"))
}

func TestImportsSubtree(t *testing.T) {
	src := `package main

import (
	_ "example.com/kit/patterns/accordion"
	_ "example.com/kit/patterns/tree"
	_ "example.com/kit/patterns/treeview"
)

func main() {}
`
	n := &upgrade.Note{Find: upgrade.Find{Imports: []string{"example.com/kit/patterns/..."}}}
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{"main.go": src}), n)
	wantHits(t, res, n,
		hitAt(src, `"example.com/kit/patterns/accordion"`, "main.go", "import example.com/kit/patterns/accordion"),
		hitAt(src, `"example.com/kit/patterns/tree"`, "main.go", "import example.com/kit/patterns/tree"),
		hitAt(src, `"example.com/kit/patterns/treeview"`, "main.go", "import example.com/kit/patterns/treeview"))
}

func TestImportsSharedPrefixSilent(t *testing.T) {
	src := `package main

import (
	_ "example.com/kit/patterns/treeview"
)

func main() {}
`
	// An exact entry for patterns/tree must not match patterns/treeview,
	// and neither may a subtree entry: ".../tree/..." stops at the "/".
	n := &upgrade.Note{Find: upgrade.Find{Imports: []string{"example.com/kit/patterns/tree"}}}
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{"main.go": src}), n)
	wantHits(t, res, n)
	n2 := &upgrade.Note{Find: upgrade.Find{Imports: []string{"example.com/kit/patterns/tree/..."}}}
	res2 := mustRun(t, newWorkspace(t, defaultKit, map[string]string{"main.go": src}), n2)
	wantHits(t, res2, n2)
}

func TestFieldsMapKeySplitLines(t *testing.T) {
	src := `package main

import "example.com/kit/ui"

func main() {
	_ = ui.ButtonConfig{
		ExtraAttrs: ui.Attrs{
			"disabled": "",
		},
	}
}
`
	n := &upgrade.Note{Find: upgrade.Find{Fields: []upgrade.FieldMatch{{
		Field: upgrade.Symbol{Pkg: "example.com/kit/ui", Name: "ButtonConfig", Member: "ExtraAttrs"},
		Key:   "disabled",
	}}}}
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{"main.go": src}), n)
	wantHits(t, res, n,
		hitAt(src, `"disabled"`, "main.go", "field example.com/kit/ui.ButtonConfig.ExtraAttrs key disabled"))
}

func TestFieldsOtherKeySilent(t *testing.T) {
	src := `package main

import "example.com/kit/ui"

func main() {
	_ = ui.ButtonConfig{
		ExtraAttrs: ui.Attrs{
			"readonly": "",
		},
	}
}
`
	n := &upgrade.Note{Find: upgrade.Find{Fields: []upgrade.FieldMatch{{
		Field: upgrade.Symbol{Pkg: "example.com/kit/ui", Name: "ButtonConfig", Member: "ExtraAttrs"},
		Key:   "disabled",
	}}}}
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{"main.go": src}), n)
	wantHits(t, res, n)
}

func TestFieldsValueRegex(t *testing.T) {
	src := `package main

import "example.com/kit/ui"

func main() {
	_ = ui.FormConfig{Action: "javascript:alert(1)"}
	_ = ui.FormConfig{Action: "/safe/path"}
}
`
	n := &upgrade.Note{Find: upgrade.Find{Fields: []upgrade.FieldMatch{{
		Field: upgrade.Symbol{Pkg: "example.com/kit/ui", Name: "FormConfig", Member: "Action"},
		Value: regexp.MustCompile(`^(javascript:|//)`),
	}}}}
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{"main.go": src}), n)
	wantHits(t, res, n,
		hitAt(src, `"javascript:alert(1)"`, "main.go", "field example.com/kit/ui.FormConfig.Action value"))
}

func TestFieldsOtherStructSilent(t *testing.T) {
	src := `package main

import "example.com/kit/ui"

func main() {
	_ = ui.OtherConfig{Action: "javascript:alert(1)"}
}
`
	n := &upgrade.Note{Find: upgrade.Find{Fields: []upgrade.FieldMatch{{
		Field: upgrade.Symbol{Pkg: "example.com/kit/ui", Name: "FormConfig", Member: "Action"},
		Value: regexp.MustCompile(`^(javascript:|//)`),
	}}}}
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{"main.go": src}), n)
	wantHits(t, res, n)
}
