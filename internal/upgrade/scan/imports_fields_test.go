package scan

import (
	"regexp"
	"strconv"
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

func keyWantField() upgrade.FieldMatch {
	return upgrade.FieldMatch{
		Field: upgrade.Symbol{Pkg: "example.com/kit/ui", Name: "ButtonConfig", Member: "ExtraAttrs"},
		Key:   "disabled",
	}
}

func TestFieldsKeyLocalVarFollow(t *testing.T) {
	src := `package main

import "example.com/kit/ui"

func main() {
	attrs := map[string]string{"disabled": ""}
	_ = ui.ButtonConfig{ExtraAttrs: attrs}
}
`
	// The map literal is one level away, in the variable's initialiser.
	n := &upgrade.Note{Find: upgrade.Find{Fields: []upgrade.FieldMatch{keyWantField()}}}
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{"main.go": src}), n)
	wantHits(t, res, n,
		hitAt(src, `"disabled"`, "main.go", "field example.com/kit/ui.ButtonConfig.ExtraAttrs key disabled"))
}

func TestFieldsKeySelectorAssign(t *testing.T) {
	src := `package main

import "example.com/kit/ui"

func main() {
	var cfg ui.ButtonConfig
	cfg.ExtraAttrs = map[string]string{"disabled": ""}
}
`
	// An assignment to the field is a use of the key too.
	n := &upgrade.Note{Find: upgrade.Find{Fields: []upgrade.FieldMatch{keyWantField()}}}
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{"main.go": src}), n)
	wantHits(t, res, n,
		hitAt(src, `"disabled"`, "main.go", "field example.com/kit/ui.ButtonConfig.ExtraAttrs key disabled"))
}

func TestFieldsKeyIndexWrite(t *testing.T) {
	src := `package main

import "example.com/kit/ui"

func main() {
	var cfg ui.ButtonConfig
	cfg.ExtraAttrs["disabled"] = ""
}
`
	// Writing the key into the field's map is the same key use.
	n := &upgrade.Note{Find: upgrade.Find{Fields: []upgrade.FieldMatch{keyWantField()}}}
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{"main.go": src}), n)
	wantHits(t, res, n,
		hitAt(src, `"disabled"`, "main.go", "field example.com/kit/ui.ButtonConfig.ExtraAttrs key disabled"))
}

func TestFieldsValueSelectorAssign(t *testing.T) {
	src := `package main

import "example.com/kit/ui"

func main() {
	var cfg ui.FormConfig
	cfg.Action = "javascript:alert(1)"
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

func TestFieldsVarReassignedSilent(t *testing.T) {
	src := `package main

import "example.com/kit/ui"

func main() {
	attrs := map[string]string{"disabled": ""}
	attrs = map[string]string{"readonly": ""}
	_ = ui.ButtonConfig{ExtraAttrs: attrs}
}
`
	// A reassigned variable's value at the use cannot be known; the
	// follow stops and the want stays silent.
	n := &upgrade.Note{Find: upgrade.Find{Fields: []upgrade.FieldMatch{keyWantField()}}}
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{"main.go": src}), n)
	wantHits(t, res, n)
}

func TestFieldsKeyVarOtherFile(t *testing.T) {
	attrs := "package main\n\nvar attrs = map[string]string{\"disabled\": \"\"}\n"
	src := `package main

import "example.com/kit/ui"

func main() {
	_ = ui.ButtonConfig{ExtraAttrs: attrs}
}
`
	// The hit lands on the key, in the file that holds it.
	n := &upgrade.Note{Find: upgrade.Find{Fields: []upgrade.FieldMatch{keyWantField()}}}
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{"main.go": src, "attrs.go": attrs}), n)
	wantHits(t, res, n,
		hitAt(attrs, `"disabled"`, "attrs.go", "field example.com/kit/ui.ButtonConfig.ExtraAttrs key disabled"))
}

func TestFieldsKeyVarOtherPkg(t *testing.T) {
	attrs := "package kitx\n\nvar Attrs = map[string]string{\"disabled\": \"\"}\n"
	src := `package main

import (
	"example.com/app/kitx"
	"example.com/kit/ui"
)

func main() {
	_ = ui.ButtonConfig{ExtraAttrs: kitx.Attrs}
}
`
	// A qualified variable in another package is followed the same way.
	n := &upgrade.Note{Find: upgrade.Find{Fields: []upgrade.FieldMatch{keyWantField()}}}
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{"main.go": src, "kitx/kitx.go": attrs}), n)
	wantHits(t, res, n,
		hitAt(attrs, `"disabled"`, "kitx/kitx.go", "field example.com/kit/ui.ButtonConfig.ExtraAttrs key disabled"))
}

func TestFieldsValueVarFollow(t *testing.T) {
	src := `package main

import "example.com/kit/ui"

func main() {
	action := "javascript:alert(1)"
	_ = ui.FormConfig{Action: action}
}
`
	// A variable has no constant value of its own; its initialiser does.
	n := &upgrade.Note{Find: upgrade.Find{Fields: []upgrade.FieldMatch{{
		Field: upgrade.Symbol{Pkg: "example.com/kit/ui", Name: "FormConfig", Member: "Action"},
		Value: regexp.MustCompile(`^(javascript:|//)`),
	}}}}
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{"main.go": src}), n)
	wantHits(t, res, n,
		hitAt(src, "action}", "main.go", "field example.com/kit/ui.FormConfig.Action value"))
}

func TestFieldsVarReassignedOtherPkgSilent(t *testing.T) {
	attrs := "package kitx\n\nvar Attrs = map[string]string{\"disabled\": \"\"}\n"
	src := `package main

import (
	"example.com/app/kitx"
	"example.com/kit/ui"
)

func main() {
	kitx.Attrs = map[string]string{}
	_ = ui.ButtonConfig{ExtraAttrs: kitx.Attrs}
}
`
	// Reassigned through its package: the initialiser is not the value.
	n := &upgrade.Note{Find: upgrade.Find{Fields: []upgrade.FieldMatch{keyWantField()}}}
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{"main.go": src, "kitx/kitx.go": attrs}), n)
	wantHits(t, res, n)
}

func TestFieldsRefusedAnchor(t *testing.T) {
	refused := []string{"javascript:alert(1)", "JavaScript:alert(1)", "//evil.example/a", "data:text/html,x", "vbscript:x"}
	allowed := []string{"/customers", "https://example.com/a", "?q=1", "#top", "mailto:a@example.com", "customers/new"}
	src := "package main\n\nimport \"example.com/kit/ui\"\n\nfunc main() {\n"
	for _, v := range append(append([]string{}, refused...), allowed...) {
		src += "\t_ = ui.FormConfig{Action: " + strconv.Quote(v) + "}\n"
	}
	src += "}\n"
	// The predicate is the anchor policy itself: exactly the actions the
	// form refuses at render, whatever their case or scheme.
	n := &upgrade.Note{Find: upgrade.Find{Fields: []upgrade.FieldMatch{{
		Field:   upgrade.Symbol{Pkg: "example.com/kit/ui", Name: "FormConfig", Member: "Action"},
		Refused: "anchor",
	}}}}
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{"main.go": src}), n)
	var want []string
	for _, v := range refused {
		want = append(want, hitAt(src, strconv.Quote(v), "main.go", "field example.com/kit/ui.FormConfig.Action refused by anchor"))
	}
	wantHits(t, res, n, want...)
}

func TestFieldsKeyCaseFolds(t *testing.T) {
	src := `package main

import "example.com/kit/ui"

func main() {
	_ = ui.ButtonConfig{ExtraAttrs: map[string]string{"DISABLED": ""}}
	var cfg ui.ButtonConfig
	cfg.ExtraAttrs["Disabled"] = ""
	_ = ui.ButtonConfig{ExtraAttrs: map[string]string{"di\u017fabled": ""}}
}
`
	// HTML attribute names fold ASCII case; a Unicode fold (\u017f, long
	// s, folds to s) is a different attribute.
	n := &upgrade.Note{Find: upgrade.Find{Fields: []upgrade.FieldMatch{keyWantField()}}}
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{"main.go": src}), n)
	wantHits(t, res, n,
		hitAt(src, `"DISABLED"`, "main.go", "field example.com/kit/ui.ButtonConfig.ExtraAttrs key disabled"),
		hitAt(src, `"Disabled"`, "main.go", "field example.com/kit/ui.ButtonConfig.ExtraAttrs key disabled"))
}
