package appgen

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	coreyaml "github.com/DonaldMurillo/gofastr/core/yaml"
)

// A block that spells a catalog kind `type:` generates what `kind:`
// generates, and its screen file imports what it uses. The decoder reads
// such a `type:` as the block's kind, so the validator, the import scan
// and the emitter agree. `type: section` is the one overlap: it stays
// the sugar keys' plain <section>, which the node renderer draws when it
// has children.
//
// The bug: the load validated `type: card` as a card and the import scan
// asked for framework/ui only, while the emitter, which draws the catalog
// from Kind alone, wrote noderender.RenderTrustedNode(uinode.Node{...})
// into the file: "undefined: noderender". TestUnionApp compiles the same
// shapes (its type_spelled screen).
func TestTypeSpelledCatalogBlockGeneratesAsKind(t *testing.T) {
	cases := map[string]struct{ body, want, refuse string }{
		"type: card with children": {
			"      - type: card\n        children:\n          - type: paragraph\n            text: hello\n",
			"ui.Card(", "noderender."},
		"type: page_header": {
			"      - type: page_header\n        props:\n          title: Welcome\n",
			"ui.PageHeader(", "noderender."},
		"type: stat_card": {
			"      - type: stat_card\n        props:\n          label: Posts\n          source:\n            entity: posts\n",
			"ui.StatCard(", "noderender."},
		// The legacy `column=value` filter is rewritten for a stat_card
		// by kind, so the type: spelling has to be one by then.
		"type: stat_card, legacy filter": {
			"      - type: stat_card\n        props:\n          label: Open\n          source:\n            entity: posts\n            filter: title=open\n",
			`title = \"open\"`, "noderender."},
		"type: section with children": {
			"      - type: section\n        children:\n          - type: paragraph\n            text: hello\n",
			"noderender.RenderTrustedNode(", "ui.Section("},
		// Control: the kind: spellings.
		"kind: card with children": {
			"      - kind: card\n        children:\n          - type: paragraph\n            text: hello\n",
			"ui.Card(", "noderender."},
		"kind: section with children": {
			"      - kind: section\n        children:\n          - type: paragraph\n            text: hello\n",
			"ui.Section(", "noderender."},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			node, err := coreyaml.Parse(`
app:
  name: Demo
  module: example.com/demo
entities:
  - name: posts
    crud: true
    fields:
      - name: title
        type: string
screens:
  - name: home
    route: /
    body:
` + tc.body)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			bp, err := appspec.DecodeAppSpec(node)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if err := appspec.ValidateAppSpec(bp); err != nil {
				t.Fatalf("validate: %v", err)
			}
			files, err := RenderAppSpecCode(bp)
			if err != nil {
				t.Fatalf("render: %v", err)
			}

			// Names package main declares at top level, across every file.
			declared := map[string]bool{}
			fset := token.NewFileSet()
			var screen *ast.File
			var src string
			for _, f := range files {
				if !strings.HasSuffix(f.Name, ".go") || strings.Contains(f.Name, "/") {
					continue // package main is the app root
				}
				pf, err := parser.ParseFile(fset, f.Name, f.Content, parser.SkipObjectResolution)
				if err != nil {
					t.Fatalf("%s does not parse: %v", f.Name, err)
				}
				if f.Name == "screen_home.go" {
					screen, src = pf, f.Content
				}
				for _, n := range topLevelNames(pf) {
					declared[n] = true
				}
			}
			if screen == nil {
				t.Fatal("no screen_home.go generated")
			}
			if !strings.Contains(src, tc.want) {
				t.Errorf("screen_home.go does not hold %s:\n%s", tc.want, src)
			}
			if strings.Contains(src, tc.refuse) {
				t.Errorf("screen_home.go holds %s:\n%s", tc.refuse, src)
			}

			// Every package the file selects from is one it imports (or a
			// name package main declares): the file compiles.
			imported := map[string]bool{}
			for _, imp := range screen.Imports {
				p := strings.Trim(imp.Path.Value, `"`)
				n := p[strings.LastIndex(p, "/")+1:]
				if imp.Name != nil {
					n = imp.Name.Name
				}
				imported[n] = true
			}
			for _, pkg := range []string{"noderender", "uinode", "ui", "html", "render"} {
				used := false
				ast.Inspect(screen, func(n ast.Node) bool {
					if sel, ok := n.(*ast.SelectorExpr); ok {
						if id, ok := sel.X.(*ast.Ident); ok && id.Name == pkg {
							used = true
						}
					}
					return true
				})
				if used && !imported[pkg] && !declared[pkg] {
					t.Errorf("screen_home.go uses %s.* but does not import it: the generated app fails to compile (undefined: %s)", pkg, pkg)
				}
			}
		})
	}
}
