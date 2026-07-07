package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestComponentGalleryCoversUI keeps the /components gallery honest: every
// exported component constructor in framework/ui (a `func Name(cfg NameConfig,
// …)`) must either appear in componentCatalog or be listed in
// notShowcased with a reason. Without this, new components land in framework/ui
// and the gallery — plus docs/content/ui-new-components.md, which claims to
// index "every UI component" — silently fall behind. When this fails, either
// add a catalog entry in components.go or an allow-list line below.
func TestComponentGalleryCoversUI(t *testing.T) {
	// Constructors deliberately not given their own gallery tile, each with
	// the reason. Aliases are shown under a different display name; chrome /
	// layout / utilities aren't standalone tiles.
	notShowcased := map[string]string{
		"AspectRatioComponent":    "shown as AspectRatio (slug aspectratio)",
		"RatingInput":             "shown as Rating (slug rating)",
		"SignalToggle":            "shown as Toggle Switch (slug toggle)",
		"SkeletonAvatar":          "shown together as SkeletonPresets (slug skeleton)",
		"SkeletonCard":            "shown together as SkeletonPresets (slug skeleton)",
		"SkeletonRow":             "shown together as SkeletonPresets (slug skeleton)",
		"ConditionalFieldVisible": "variant of ConditionalField (slug conditionalfield)",
		"RadioGroup":              "grouped variant of Radio (slug radio)",
		"LinkButton":              "styling variant of Button/Link",
		"Responsive":              "layout utility wrapper, no standalone visual",
		"Section":                 "semantic layout primitive, shown via layouts",
		"SidebarBody":             "internal building block of Sidebar",
		"DocLayout":               "full-page doc skeleton; shown in-context, not as a tile",
		"SiteHeader":              "page chrome; shown on every site page",
		"SiteFooter":              "page chrome; shown on every site page",
		"AnchoredRail":            "in-page scrollspy rail; needs a scrollable page to demo",
		"SignOut":                 "auth action button; needs an auth session to demo",
	}

	// Gallery display names, normalized (lowercase, spaces stripped).
	shown := map[string]bool{}
	for _, e := range componentCatalog {
		shown[normalizeName(e.Name)] = true
	}

	uiDir := filepath.Join("..", "..", "framework", "ui")
	entries, err := os.ReadDir(uiDir)
	if err != nil {
		t.Fatalf("read framework/ui: %v", err)
	}
	fset := token.NewFileSet()
	var missing []string
	for _, de := range entries {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".go") || strings.HasSuffix(de.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(uiDir, de.Name()), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", de.Name(), err)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || !fn.Name.IsExported() {
				continue
			}
			if !isComponentConstructor(fn) {
				continue
			}
			name := fn.Name.Name
			if shown[normalizeName(name)] {
				continue
			}
			if _, ok := notShowcased[name]; ok {
				continue
			}
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		t.Errorf("framework/ui components not in the gallery and not allow-listed: %v\n"+
			"Add a componentCatalog entry in components.go (and a line in docs/content/ui-new-components.md), "+
			"or add the name to notShowcased with a reason.", missing)
	}
}

// isComponentConstructor reports whether fn looks like a component
// constructor: its first parameter's type is a `<Something>Config` struct.
// That excludes helpers (BaseCSS, RegisterIcon, HighlightLines, …) which take
// no Config, while catching every real component including variadic-children
// ones like Card(cfg, …) and TerminalBlock(cfg, …).
func isComponentConstructor(fn *ast.FuncDecl) bool {
	if fn.Type.Params == nil || len(fn.Type.Params.List) == 0 {
		return false
	}
	ident, ok := fn.Type.Params.List[0].Type.(*ast.Ident)
	if !ok {
		return false
	}
	return strings.HasSuffix(ident.Name, "Config")
}

func normalizeName(s string) string {
	return strings.ToLower(strings.ReplaceAll(s, " ", ""))
}
