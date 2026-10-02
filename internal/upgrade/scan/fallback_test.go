package scan

import (
	"go/token"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/upgrade"
	"golang.org/x/tools/go/packages"
)

// kitV2 is the stub kit after a hypothetical v2: SiteHeader and
// Layout.WithHeader are gone, and so is package patterns/accordion.
var kitV2 = map[string]string{
	"ui/ui.go": `package ui

type Attrs map[string]string

type Layout struct{ Title string }

type Stack[T any] struct{ Items []T }
`,
}

func TestFallbackBrokenKit(t *testing.T) {
	src := `package main

import (
	_ "example.com/kit/patterns/accordion"

	kitui "example.com/kit/ui"
)

func main() {
	_ = kitui.SiteHeader("t")
	var l *kitui.Layout
	l.WithHeader("h")
	var n int = "oops"
	_ = n
}
`
	app := map[string]string{
		"main.go": src,
		"go.mod":  "module example.com/app\n\ngo 1.27.0\n\nrequire example.com/kit v1.1.0\n\nreplace example.com/kit => ../kit\n",
	}
	root := newWorkspace(t, kitV2, app)
	n := &upgrade.Note{Find: upgrade.Find{
		Uses:    []upgrade.Symbol{siteHeaderSym, withHeaderSym},
		Imports: []string{"example.com/kit/patterns/accordion"},
	}}
	testEnv(t)
	res, err := Run(root, []*upgrade.Note{n}, upgrade.MarkerSinks{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.TypeChecked {
		t.Fatal("TypeChecked = true, want false for a broken kit")
	}
	if len(res.Broken) != 1 || res.Broken[0] != "example.com/app" {
		t.Fatalf("Broken = %v, want [example.com/app]", res.Broken)
	}
	// The uses and imports hits land at the error positions; the unrelated
	// compile error is unexplained.
	wantSite := hitAt(src, "SiteHeader", "main.go", siteHeaderSym.String())
	wantHeader := hitAt(src, `WithHeader("h")`, "main.go", withHeaderSym.String())
	wantImport := hitAt(src, `"example.com/kit/patterns/accordion"`, "main.go", "import example.com/kit/patterns/accordion")
	got := hitStrs(res.Hits[n])
	if len(got) != 3 {
		t.Fatalf("hits = %v, want 3 (2 uses + 1 import)", got)
	}
	for _, w := range []string{wantSite, wantHeader, wantImport} {
		found := false
		for _, g := range got {
			if g == w {
				found = true
			}
		}
		if !found {
			t.Fatalf("hits = %v, want %q among them", got, w)
		}
	}
	if len(res.Unexplained) != 1 {
		t.Fatalf("Unexplained = %v, want exactly the unrelated error", hitStrs(res.Unexplained))
	}
	ue := res.Unexplained[0]
	if ue.File != "main.go" || ue.Line != lineOf(src, `"oops"`) || ue.Why == "" {
		t.Fatalf("Unexplained = %+v, want main.go at the \"oops\" line", ue)
	}
}

// runBroken scans an app over kitV2 with one note and the default
// hermetic environment.
func runBroken(t *testing.T, src string, n *upgrade.Note) *Result {
	t.Helper()
	root := newWorkspace(t, kitV2, map[string]string{"main.go": src})
	testEnv(t)
	res, err := Run(root, []*upgrade.Note{n}, upgrade.MarkerSinks{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.TypeChecked {
		t.Fatal("TypeChecked = true, want false")
	}
	return res
}

// TestFallbackNestedModulePaths proves a nested module's compile errors
// land on root-relative paths: the go command echoes them relative to
// the module the load ran in, tools/ here, not the root module.
func TestFallbackNestedModulePaths(t *testing.T) {
	src := "package main\n\nimport \"example.com/kit/ui\"\n\nfunc main() { _ = ui.SiteHeader(\"t\") }\n"
	// The root's main.go is long enough that a tools/ position joined
	// onto the root module still names a line in it.
	rootMain := "package main\n\n\n\n\n\nfunc main() {}\n"
	root := newWorkspace(t, kitV2, map[string]string{"main.go": rootMain})
	nestedTools(t, root, map[string]string{"main.go": src})
	n := &upgrade.Note{Find: upgrade.Find{Uses: []upgrade.Symbol{siteHeaderSym}}}
	testEnv(t)
	res, err := Run(root, []*upgrade.Note{n}, upgrade.MarkerSinks{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, h := range res.Hits[n] {
		if h.File != "tools/main.go" {
			t.Errorf("hit at %s:%d (%s), want tools/main.go", h.File, h.Line, h.Why)
		}
	}
	if len(res.Hits[n]) == 0 {
		t.Error("no hits in the nested module")
	}
	for _, u := range res.Unexplained {
		t.Errorf("unexplained: %s:%d: %s", u.File, u.Line, u.Why)
	}
}

func TestFallbackLongerNameSilent(t *testing.T) {
	src := `package main

import "example.com/kit/ui"

func main() { _ = ui.SiteHeaderConfig{} }
`
	n := &upgrade.Note{Find: upgrade.Find{Uses: []upgrade.Symbol{siteHeaderSym}}}
	res := runBroken(t, src, n)
	if len(res.Hits[n]) != 0 {
		t.Fatalf("hits = %v, want none: SiteHeaderConfig is not SiteHeader", hitStrs(res.Hits[n]))
	}
	if len(res.Unexplained) != 1 {
		t.Fatalf("Unexplained = %v, want the undefined SiteHeaderConfig", hitStrs(res.Unexplained))
	}
}

func TestFallbackSignatureChangeExplained(t *testing.T) {
	kit := map[string]string{"ui/ui.go": "package ui\n\ntype Layout struct{}\n\nfunc (l *Layout) WithHeader(h, sub string) {}\n"}
	src := `package main

import "example.com/kit/ui"

func main() {
	var l ui.Layout
	l.WithHeader("h")
}
`
	root := newWorkspace(t, kit, map[string]string{"main.go": src})
	testEnv(t)
	n := &upgrade.Note{Find: upgrade.Find{Uses: []upgrade.Symbol{withHeaderSym}}}
	res, err := Run(root, []*upgrade.Note{n}, upgrade.MarkerSinks{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// "not enough arguments in call to l.WithHeader" names no package; the
	// typed uses hit on the same line explains it.
	wantHits(t, res, n, hitAt(src, `WithHeader("h")`, "main.go", withHeaderSym.String()))
	if err := res.Hits[n][0].Err; err != "" {
		t.Fatalf("Err = %q, want empty: the typed matcher resolved the call", err)
	}
	if len(res.Unexplained) != 0 {
		t.Fatalf("Unexplained = %v, want none", hitStrs(res.Unexplained))
	}
}

func TestFallbackAliasSpellingExplained(t *testing.T) {
	// framework.EntityConfig is an alias of entity.EntityConfig: the note
	// names the declaring type, go/types names the alias the app wrote.
	kit := map[string]string{
		"entity/entity.go": "package entity\n\ntype Config struct{ Name string }\n",
		"ui/ui.go":         "package ui\n\nimport \"example.com/kit/entity\"\n\ntype EntityConfig = entity.Config\n",
	}
	src := `package main

import "example.com/kit/ui"

func main() { _ = ui.EntityConfig{Name: "t", Public: true} }
`
	root := newWorkspace(t, kit, map[string]string{"main.go": src})
	testEnv(t)
	sym := upgrade.Symbol{Pkg: "example.com/kit/entity", Name: "Config", Member: "Public"}
	n := &upgrade.Note{Find: upgrade.Find{Uses: []upgrade.Symbol{sym}}}
	res, err := Run(root, []*upgrade.Note{n}, upgrade.MarkerSinks{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.TypeChecked {
		t.Fatal("TypeChecked = true, want the unknown field to break the build")
	}
	wantHits(t, res, n, hitAt(src, `Public: true`, "main.go", sym.String()))
	if len(res.Unexplained) != 0 {
		t.Fatalf("Unexplained = %v, want none", hitStrs(res.Unexplained))
	}
}

func TestFallbackFieldEntryExplained(t *testing.T) {
	kit := map[string]string{"entity/entity.go": "package entity\n\ntype Config struct{ Name string }\n"}
	src := `package main

import "example.com/kit/entity"

func main() {
	_ = entity.Config{Public: true}
	_ = entity.Config{Access: "x"}
}
`
	root := newWorkspace(t, kit, map[string]string{"main.go": src})
	testEnv(t)
	public := upgrade.Symbol{Pkg: "example.com/kit/entity", Name: "Config", Member: "Public"}
	access := upgrade.Symbol{Pkg: "example.com/kit/entity", Name: "Config", Member: "Access"}
	n := &upgrade.Note{Find: upgrade.Find{Fields: []upgrade.FieldMatch{
		{Field: public},
		// A conditioned entry cannot be checked against an error.
		{Field: access, Value: regexp.MustCompile("^y$")},
	}}}
	res, err := Run(root, []*upgrade.Note{n}, upgrade.MarkerSinks{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	wantHits(t, res, n, hitAt(src, `Public: true`, "main.go", "field "+public.String()))
	if err := res.Hits[n][0].Err; !strings.Contains(err, "unknown field Public") {
		t.Fatalf("Err = %q, want the compile error the hit was read from", err)
	}
	if len(res.Unexplained) != 1 || res.Unexplained[0].Line != lineOf(src, `Access: "x"`) {
		t.Fatalf("Unexplained = %v, want only the conditioned Access field", hitStrs(res.Unexplained))
	}
}

func TestFallbackMissingModuleUnexplained(t *testing.T) {
	app := map[string]string{
		"main.go": "package main\n\nimport _ \"example.com/missing\"\n\nfunc main() {}\n",
		"go.mod":  "module example.com/app\n\ngo 1.27.0\n\nrequire example.com/missing v1.0.0\n",
	}
	root := newWorkspace(t, defaultKit, app)
	testEnv(t)
	res, err := Run(root, nil, upgrade.MarkerSinks{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.TypeChecked || len(res.Unexplained) == 0 {
		t.Fatalf("TypeChecked=%v Unexplained=%v, want the unresolvable module reported", res.TypeChecked, hitStrs(res.Unexplained))
	}
	for _, h := range res.Unexplained {
		if strings.Contains(h.Why, "example.com/missing") {
			return
		}
	}
	t.Fatalf("Unexplained = %v, want an entry naming example.com/missing", hitStrs(res.Unexplained))
}

func TestRunNoGoMod(t *testing.T) {
	testEnv(t)
	if _, err := Run(t.TempDir(), nil, upgrade.MarkerSinks{}); err == nil {
		t.Fatal("Run returned nil error with no go.mod at or above root")
	}
}

func TestFallbackPositionlessErrorKept(t *testing.T) {
	e := &engine{goHitLines: map[lineKey]bool{}, hits: map[*upgrade.Note][]Hit{}, broken: map[string]bool{}}
	e.fallbackPackage(&packages.Package{
		Fset:   token.NewFileSet(),
		Errors: []packages.Error{{Pos: "", Msg: "go: inconsistent vendoring"}},
	})
	res := e.result()
	if len(res.Unexplained) != 1 || res.Unexplained[0].Why != "go: inconsistent vendoring" || res.Unexplained[0].File != "" {
		t.Fatalf("Unexplained = %+v, want the positionless error with no file", res.Unexplained)
	}
}

func TestFallbackQuotedPathFieldExplained(t *testing.T) {
	// Two imports sharing the name html make go/types write the type
	// with its quoted import path; qualified must spell that too.
	kit := map[string]string{"html/html.go": "package html\n\ntype DetailsConfig struct{ Title string }\n"}
	src := `package main

import (
	stdhtml "html"

	"example.com/kit/html"
)

func main() {
	_ = stdhtml.EscapeString("x")
	_ = html.DetailsConfig{Title: "t", Disclosure: true}
}
`
	root := newWorkspace(t, kit, map[string]string{"main.go": src})
	testEnv(t)
	sym := upgrade.Symbol{Pkg: "example.com/kit/html", Name: "DetailsConfig", Member: "Disclosure"}
	n := &upgrade.Note{Find: upgrade.Find{Fields: []upgrade.FieldMatch{{Field: sym}}}}
	res, err := Run(root, []*upgrade.Note{n}, upgrade.MarkerSinks{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	wantHits(t, res, n, hitAt(src, "Disclosure: true", "main.go", "field "+sym.String()))
	if err := res.Hits[n][0].Err; !strings.Contains(err, "unknown field Disclosure") {
		t.Fatalf("Err = %q, want the compile error the hit was read from", err)
	}
	if len(res.Unexplained) != 0 {
		t.Fatalf("Unexplained = %v, want none", hitStrs(res.Unexplained))
	}
}

func TestFallbackDotImportUseExplained(t *testing.T) {
	// A dot import writes the symbol bare: "undefined: SiteHeader".
	src := `package main

import . "example.com/kit/ui"

func main() {
	var _ Stack[int]
	_ = SiteHeader("t")
}
`
	n := &upgrade.Note{Find: upgrade.Find{Uses: []upgrade.Symbol{siteHeaderSym}}}
	res := runBroken(t, src, n)
	wantHits(t, res, n, hitAt(src, "SiteHeader", "main.go", siteHeaderSym.String()))
	if err := res.Hits[n][0].Err; !strings.Contains(err, "undefined: SiteHeader") {
		t.Fatalf("Err = %q, want the compile error the hit was read from", err)
	}
	if len(res.Unexplained) != 0 {
		t.Fatalf("Unexplained = %v, want none", hitStrs(res.Unexplained))
	}
}

func TestFallbackDotImportFieldExplained(t *testing.T) {
	// go/types still qualifies the type with its package name under a
	// dot import, so the declared-name spelling matches; the bare
	// spelling is what a bare message would need.
	kit := map[string]string{"html/html.go": "package html\n\ntype DetailsConfig struct{ Title string }\n"}
	src := `package main

import . "example.com/kit/html"

func main() { _ = DetailsConfig{Title: "t", Disclosure: true} }
`
	root := newWorkspace(t, kit, map[string]string{"main.go": src})
	testEnv(t)
	sym := upgrade.Symbol{Pkg: "example.com/kit/html", Name: "DetailsConfig", Member: "Disclosure"}
	n := &upgrade.Note{Find: upgrade.Find{Fields: []upgrade.FieldMatch{{Field: sym}}}}
	res, err := Run(root, []*upgrade.Note{n}, upgrade.MarkerSinks{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	wantHits(t, res, n, hitAt(src, "Disclosure: true", "main.go", "field "+sym.String()))
	if len(res.Unexplained) != 0 {
		t.Fatalf("Unexplained = %v, want none", hitStrs(res.Unexplained))
	}
}

func TestFallbackUnrelatedErrorKept(t *testing.T) {
	// A typed hit explains only an error that names its symbol; the
	// undefined helper beside the SiteHeader call stays in the report.
	kit := map[string]string{"ui/ui.go": "package ui\n\nfunc SiteHeader(t string) string { return t }\n"}
	src := `package main

import "example.com/kit/ui"

func main() { _ = ui.SiteHeader("t") + somethingRemoved() }
`
	root := newWorkspace(t, kit, map[string]string{"main.go": src})
	testEnv(t)
	n := &upgrade.Note{Find: upgrade.Find{Uses: []upgrade.Symbol{siteHeaderSym}}}
	res, err := Run(root, []*upgrade.Note{n}, upgrade.MarkerSinks{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	wantHits(t, res, n, hitAt(src, "SiteHeader", "main.go", siteHeaderSym.String()))
	if err := res.Hits[n][0].Err; err != "" {
		t.Fatalf("Err = %q, want empty: the typed matcher resolved the call", err)
	}
	if len(res.Unexplained) != 1 || !strings.Contains(res.Unexplained[0].Why, "undefined: somethingRemoved") {
		t.Fatalf("Unexplained = %v, want the unrelated undefined symbol", hitStrs(res.Unexplained))
	}
}

func TestFallbackFieldTypeChangeExplained(t *testing.T) {
	// A changed field type names neither the type nor the field: the
	// error is about the struct literal the hit's field sits in.
	kit := map[string]string{"ui/ui.go": "package ui\n\ntype FormFieldConfig struct{ Input func() string }\n"}
	src := `package main

import "example.com/kit/ui"

func main() { _ = ui.FormFieldConfig{Input: "oops"} }
`
	root := newWorkspace(t, kit, map[string]string{"main.go": src})
	testEnv(t)
	sym := upgrade.Symbol{Pkg: "example.com/kit/ui", Name: "FormFieldConfig", Member: "Input"}
	n := &upgrade.Note{Find: upgrade.Find{Uses: []upgrade.Symbol{sym}}}
	res, err := Run(root, []*upgrade.Note{n}, upgrade.MarkerSinks{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	wantHits(t, res, n, hitAt(src, `Input: "oops"`, "main.go", sym.String()))
	if err := res.Hits[n][0].Err; err != "" {
		t.Fatalf("Err = %q, want empty: the typed matcher resolved the field key", err)
	}
	if len(res.Unexplained) != 0 {
		t.Fatalf("Unexplained = %v, want none: the mismatch is the field's type change", hitStrs(res.Unexplained))
	}
}

func TestFallbackLocalAliasFieldExplained(t *testing.T) {
	// go/types writes a type alias declared in the erroring package
	// bare, under the alias name: "... of type LocalConfig".
	kit := map[string]string{"html/html.go": "package html\n\ntype DetailsConfig struct{ Title string }\n"}
	src := `package main

import "example.com/kit/html"

type LocalConfig = html.DetailsConfig

func main() { _ = LocalConfig{Title: "t", Disclosure: true} }
`
	root := newWorkspace(t, kit, map[string]string{"main.go": src})
	testEnv(t)
	sym := upgrade.Symbol{Pkg: "example.com/kit/html", Name: "DetailsConfig", Member: "Disclosure"}
	n := &upgrade.Note{Find: upgrade.Find{Fields: []upgrade.FieldMatch{{Field: sym}}}}
	res, err := Run(root, []*upgrade.Note{n}, upgrade.MarkerSinks{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	wantHits(t, res, n, hitAt(src, "Disclosure: true", "main.go", "field "+sym.String()))
	if len(res.Unexplained) != 0 {
		t.Fatalf("Unexplained = %v, want none", hitStrs(res.Unexplained))
	}
}
