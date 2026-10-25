package appgen

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/framework"
)

// PR #134 fixed the e2e-test emitters; the entity and screen emitters were
// never audited for the same class. The property is one sentence:
//
//	No app-spec-derived string may terminate the Go literal it is emitted
//	into.
//
// This is not hypothetical developer input. The documented workflow has an
// AGENT authoring gofastr.yml from natural-language requirements, so every
// label, title and route in the spec is transcribed text, and `gofastr
// generate` writes Go the developer then builds and runs.
//
// The sweep drives real payloads through every IR field the emitters read and
// requires each to be either REJECTED at validate time or emitted inertly.
// Silent acceptance that lands executable Go is the finding.

// goLiteralBreakers are the byte sequences that end a Go string literal. A raw
// (backtick) literal has no escape mechanism at all, so one backtick closes
// it; an interpreted literal ends at an unescaped quote and never spans a
// newline.
var goLiteralBreakers = []string{
	"x`+PWN()+`y",
	`x"+PWN()+"y`,
	"x\nfunc PWN() {}\nvar y = \"",
	"x\r\nPWN()",
}

func TestAppSpecEmittersRejectOrNeutralizeLiteralBreakers(t *testing.T) {
	for _, payload := range goLiteralBreakers {
		label := strings.NewReplacer("\n", "N", "\r", "R", "`", "BT", `"`, "Q").Replace(payload)
		for _, tc := range appSpecPayloadSites(payload) {
			t.Run(tc.site+"/"+label, func(t *testing.T) {
				if err := appspec.ValidateAppSpec(tc.bp); err != nil {
					return // rejected at the boundary, the documented fix
				}
				files, err := RenderAppSpecFiles(tc.bp)
				if err != nil {
					return // the emitter refused; also fine
				}
				for _, f := range files {
					if strings.HasSuffix(f.Name, ".go") {
						assertPayloadStayedData(t, tc.site, f.Name, f.Content)
					}
				}
			})
		}
	}
}

// assertPayloadStayedData parses the emitted file and fails if the marker
// became syntax rather than data.
//
// Parsing is the decisive check: an injected `func PWN()` is perfectly valid
// Go, so "does it compile" would pass it. The question is whether the marker
// appears as an IDENTIFIER. Inside a string literal it is just the label the
// spec asked for.
func assertPayloadStayedData(t *testing.T, site, name, src string) {
	t.Helper()
	if !strings.Contains(src, "PWN") {
		return
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, name, src, parser.AllErrors)
	if err != nil {
		// Output that does not parse is a bug too: a spec string must not be
		// able to make the generator emit broken source.
		t.Fatalf("SECURITY: [injection] %s: emitted %s does not parse: %v", site, name, err)
	}
	var escaped bool
	ast.Inspect(file, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && strings.Contains(id.Name, "PWN") {
			escaped = true
			return false
		}
		return true
	})
	if escaped {
		t.Fatalf("SECURITY: [injection] %s: the payload left its literal and became an identifier in %s", site, name)
	}
}

type payloadSite struct {
	site string
	bp   appspec.AppSpec
}

func appSpecPayloadSites(payload string) []payloadSite {
	base := func() appspec.AppSpec {
		return appspec.AppSpec{
			App: appspec.AppSpecApp{Name: "app"},
			Entities: []framework.EntityDeclaration{{
				Name:   "tickets",
				Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}},
			}},
			Screens: []appspec.AppSpecScreen{{Name: "home", Route: "/", Type: "page"}},
		}
	}
	var out []payloadSite
	add := func(site string, mut func(*appspec.AppSpec)) {
		bp := base()
		mut(&bp)
		out = append(out, payloadSite{site, bp})
	}

	add("app.name", func(b *appspec.AppSpec) { b.App.Name = payload })
	add("app.description", func(b *appspec.AppSpec) { b.App.Description = payload })
	add("entity.table", func(b *appspec.AppSpec) { b.Entities[0].Table = payload })
	add("field.enum", func(b *appspec.AppSpec) { b.Entities[0].Fields[0].Values = []string{payload} })
	add("screen.route", func(b *appspec.AppSpec) { b.Screens[0].Route = "/" + payload })
	add("screen.title", func(b *appspec.AppSpec) { b.Screens[0].Title = payload })
	add("screen.description", func(b *appspec.AppSpec) { b.Screens[0].Description = payload })
	add("block.text", func(b *appspec.AppSpec) {
		b.Screens[0].Body = []appspec.AppSpecBlock{{Kind: "text", Text: payload}}
	})
	add("block.heading", func(b *appspec.AppSpec) {
		b.Screens[0].Body = []appspec.AppSpecBlock{{Kind: "heading", Level: 1, Text: payload}}
	})
	add("block.class", func(b *appspec.AppSpec) {
		b.Screens[0].Body = []appspec.AppSpecBlock{{Kind: "text", Text: "hi", Class: payload}}
	})
	add("block.href", func(b *appspec.AppSpec) {
		b.Screens[0].Body = []appspec.AppSpecBlock{{Kind: "text", Text: "hi", Href: payload}}
	})
	add("block.empty_text", func(b *appspec.AppSpec) {
		b.Screens[0].Body = []appspec.AppSpecBlock{{Kind: "entity_list", Entity: "tickets", EmptyText: payload}}
	})
	return out
}
