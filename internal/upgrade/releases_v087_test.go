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

// v087Fixture is an app written against v0.86.0 vocabulary: every
// marked line is one the three prefix / hook notes must list, and the
// unmarked lines (kept framework-module keys, framework/ui's own fui-*
// classes, a --fui-* token, migrated cui-* spellings) must stay silent.
var v087Fixture = map[string]string{
	"views.go": `package app

import "github.com/DonaldMurillo/gofastr/core-ui/html"

var rpcAttr = html.Attrs{"data-fui-rpc": "/save"} // M1 attr key
var openAttr = html.Attrs{"data-fui-open": "nav"} // M2 attr key
var markup = "<div data-fui-signal-set=\"x\"></div>" // M3 attr in markup
var widget = "fui-widget fui-pos-top-right" // M4 classes
var themeClass = "fui-theme-a1b2c3d4" // M5 dynamic class
var copied = "fui-copied" // M6 kernel copy class
var stack = "fui-toast-stack-auto" // M7 toast singleton
var keptLightbox = html.Attrs{"data-fui-lightbox": "1"}
var keptKit = "fui-button fui-visually-hidden fui-theme-toggle fui-notification"
var migrated = html.Attrs{"data-cui-rpc": "/save"}
var migratedClass = "cui-widget"
`,
	"site.css": `.fui-widget { position: fixed } /* C1 */
.fui-dropdown__panel { padding: 0 } /* C2 */
.fui-copied { color: green } /* C3 */
.fui-toast-stack-auto { inset: 0 } /* C4 */
.fui-button { color: red }
.fui-visually-hidden { clip: rect(0 0 0 0) }
.cui-widget { position: fixed }
:root { --fui-section-menu-top: 2rem }
`,
	"static/app.js": `el.setAttribute('data-fui-rpc-after-done', 'x'); // J1
const n = root.dataset.fuiComp; // J2
el.classList.add('fui-hidden'); // J3
el.classList.toggle('fui-copied'); // J4
document.querySelector('.fui-toast-stack-auto'); // J5
el.setAttribute('data-fui-lightbox', '1');
el.setAttribute('data-fui-pane-open', '1');
el.classList.add('fui-button', 'fui-theme-toggle');
el.setAttribute('data-cui-rpc', '/x');
el.style.setProperty('--fui-section-menu-top', '1rem');
`,
	"templates/page.html": `<button data-fui-rpc="/save">Save</button> <!-- H1 -->
<div class="fui-slot fui-pos-bottom"></div> <!-- H2 -->
<span class="fui-copied"></span> <!-- H3 -->
<div class="fui-toast-stack-auto"></div> <!-- H4 -->
<img data-fui-lightbox="1" data-fui-zoomed="0">
<button class="fui-button">ok</button>
<div data-cui-rpc="/x" class="cui-widget"></div>
`,
}

// v087Marks maps a fixture mark to the file holding it.
func v087Marks(t *testing.T, marks ...string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, mark := range marks {
		found := false
		for file, src := range v087Fixture {
			if strings.Contains(src, mark) {
				out[file+":"+itoa(lineOf(t, src, mark))] = true
				found = true
			}
		}
		if !found {
			t.Fatalf("mark %q is in no fixture file", mark)
		}
	}
	return out
}

// TestV087PrefixNotesListEveryOldSpelling drives the shipped v0.87.0
// prefix and hook notes over the fixture: each marked line is a hit,
// in Go constants, CSS, JS and HTML alike, and nothing else is.
func TestV087PrefixNotesListEveryOldSpelling(t *testing.T) {
	reg, err := upgrade.Load()
	if err != nil {
		t.Fatalf("upgrade.Load: %v", err)
	}
	want := map[int]map[string]bool{
		1: v087Marks(t, "M1", "M2", "M3", "M4", "M5", "C1", "C2", "J1", "J2", "J3", "H1", "H2"),
		2: v087Marks(t, "M6", "C3", "J4", "H3"),
		3: v087Marks(t, "M7", "C4", "J5", "H4"),
	}
	kit := map[string]string{"core-ui/html/html.go": "package html\n\ntype Attrs map[string]string\n"}
	root := scantest.App(t, v087Fixture, scantest.Options{Kit: kit})
	for idx, lines := range want {
		n := scantest.Note(t, reg, "v0.87.0", idx)
		if n.Review || !n.Breaking {
			t.Errorf("note %d: every old spelling is dead, so the note is breaking and edit-tier", idx)
		}
		res := scantest.Run(t, root, []*upgrade.Note{n}, reg.MarkerSinks)
		if !res.TypeChecked {
			t.Fatalf("fixture did not type-check: broken=%v unexplained=%v", res.Broken, res.Unexplained)
		}
		got := map[string]bool{}
		for _, h := range res.Hits[n] {
			got[h.File+":"+itoa(h.Line)] = true
		}
		for line := range lines {
			if !got[line] {
				t.Errorf("note %d: no hit at %s", idx, line)
			}
		}
		for line := range got {
			if !lines[line] {
				t.Errorf("note %d: unexpected hit at %s (a kept key, a kit class or a migrated spelling)", idx, line)
			}
		}
	}
}

// TestV087ExamplesStaySilent scans the repo's migrated examples tree
// with the v0.87.0 string, css and text matchers: zero hits.
func TestV087ExamplesStaySilent(t *testing.T) {
	reg, err := upgrade.Load()
	if err != nil {
		t.Fatalf("upgrade.Load: %v", err)
	}
	var notes []*upgrade.Note
	for _, rel := range reg.Releases {
		if rel.Version != "v0.87.0" {
			continue
		}
		for _, n := range rel.Notes {
			notes = append(notes, scantest.Only(n, "strings", "css", "config", "text"))
		}
	}
	res := scantest.Run(t, "../../examples", notes, reg.MarkerSinks)
	// Zero hits on a tree that did not type-check prove nothing: the
	// attribute matcher needs the kit's Attrs type to see map keys.
	if !res.TypeChecked || len(res.Broken) != 0 {
		t.Fatalf("examples tree did not type-check: broken=%v", res.Broken)
	}
	for i, n := range notes {
		for _, h := range scantest.Hits(res, n) {
			t.Errorf("v0.87.0 note %d (%s): fires on the migrated examples tree: %s", i, n.Change, h)
		}
	}
}
