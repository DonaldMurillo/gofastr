package upgrade_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/upgrade"
	"github.com/DonaldMurillo/gofastr/internal/upgrade/scan"
	"github.com/DonaldMurillo/gofastr/internal/upgrade/scantest"
)

// The v0.86.0 notes, driven through the shipped YAML and the real scan
// engine: every string/css matcher separates an app written against
// v0.85.0 from its migrated spelling, the Go-API matchers hit the old
// symbols through a stub kit at the real import paths, the kept ui-*
// marker names stay silent, and the repo's migrated examples tree
// reports zero hits beyond three documented prose mentions.

const v086Version = "v0.86.0"

// v086Loaded returns the registry and its v0.86.0 release.
func v086Loaded(t *testing.T) (*upgrade.Registry, *upgrade.Release) {
	t.Helper()
	reg, err := upgrade.Load()
	if err != nil {
		t.Fatalf("upgrade.Load: %v", err)
	}
	for i := range reg.Releases {
		if reg.Releases[i].Version == v086Version {
			return reg, &reg.Releases[i]
		}
	}
	t.Fatalf("no %s release in the registry", v086Version)
	return nil, nil
}

// TestV086NotesConvertedOrNodetect pins that each v0.86.0 note either
// describes matchers or says why it cannot.
func TestV086NotesConvertedOrNodetect(t *testing.T) {
	_, rel := v086Loaded(t)
	for i, n := range rel.Notes {
		hasFind := !n.Find.Empty()
		reason := strings.TrimSpace(n.Nodetect)
		if hasFind == (reason != "") {
			t.Errorf("note %d (%s): must carry exactly one of find and nodetect", i, n.Change)
		}
	}
}

// lineOf returns the 1-based line holding mark's first occurrence in
// src, so expected hit lines derive from the fixture text itself.
func lineOf(t *testing.T, src, mark string) int {
	t.Helper()
	i := strings.Index(src, mark)
	if i < 0 {
		t.Fatalf("marker %q not in fixture:\n%s", mark, src)
	}
	return 1 + strings.Count(src[:i], "\n")
}

// hitLines renders a note's hits as "file:line" strings.
func hitLines(res *scan.Result, n *upgrade.Note) map[string]bool {
	out := map[string]bool{}
	for _, h := range scantest.Hits(res, n) {
		if i := strings.LastIndex(h, ":"); i >= 0 {
			out[h[:i]] = true
		}
	}
	return out
}

// v086MarkLines maps each mark to the "file:line" it sits on, searching
// the fixture set; a mark no file holds fails the test.
func v086MarkLines(t *testing.T, idx int, files map[string]string, marks []string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, mark := range marks {
		found := false
		for name, src := range files {
			if !strings.Contains(src, mark) {
				continue
			}
			if found {
				t.Fatalf("note %d: mark %q is in more than one fixture file", idx, mark)
			}
			found = true
			out[name+":"+itoa(lineOf(t, src, mark))] = true
		}
		if !found {
			t.Fatalf("note %d: mark %q is in no fixture file", idx, mark)
		}
	}
	return out
}

func itoa(n int) string { return fmt.Sprintf("%d", n) }

// v086Pair is one note's pre-migration spellings (each mark's line must
// be hit) and its migrated spellings (which must stay silent).
type v086Pair struct {
	oldFiles map[string]string
	oldMarks []string
	newFiles map[string]string
}

// v086StringPairs holds a pair for every v0.86.0 note whose find has
// string or css matchers; TestV086StringsOldVsNew fails on a note
// missing here.
var v086StringPairs = map[int]*v086Pair{
	3: { // button classes
		oldFiles: map[string]string{
			"n03.go":  "package app\n\nvar btnCls = \"ui-button ui-button--primary\"\n",
			"n03.css": ".save.ui-button { color: red }\n",
		},
		oldMarks: []string{"ui-button ui-button--primary", ".save.ui-button"},
		newFiles: map[string]string{
			"n03.go":  "package app\n\nvar btnCls = \"fui-button fui-button--primary\"\n",
			"n03.css": ".fui-button { color: red }\n",
		},
	},
	4: { // form family classes
		oldFiles: map[string]string{
			"n04.go":  "package app\n\nvar formCls = \"ui-form ui-form--block-actions\"\n",
			"n04.css": ".ui-form-field { gap: 4px }\n",
		},
		oldMarks: []string{"ui-form ui-form--block-actions", ".ui-form-field"},
		newFiles: map[string]string{
			"n04.go":  "package app\n\nvar formCls = \"fui-form fui-form--block-actions\"\n",
			"n04.css": ".fui-field { gap: 4px }\n",
		},
	},
	9: { // bespoke-behaviour family
		oldFiles: map[string]string{
			"n09.go":  "package app\n\nvar uploadRaw = \"<div class=\\\"ui-fileupload\\\" id=\\\"up\\\">\"\n\nvar zoneAttrs = map[string]string{\"data-fui-fileupload\": \"zone\"}\n",
			"n09.css": ".ui-dropzone { border: 1px solid }\n",
		},
		oldMarks: []string{`class=\"ui-fileupload\"`, "data-fui-fileupload", ".ui-dropzone"},
		newFiles: map[string]string{
			"n09.go":  "package app\n\nvar uploadRaw = \"<div class=\\\"fui-upload\\\" id=\\\"up\\\">\"\n\nvar zoneAttrs = map[string]string{\"data-hui-drop\": \"zone\"}\n",
			"n09.css": ".fui-dropzone { border: 1px solid }\n",
		},
	},
	10: { // conditional-field watched attributes
		oldFiles: map[string]string{
			"n10.go": "package app\n\nvar condAttrs = map[string]string{\n\t\"data-when-name\": \"kind\",\n\t\"data-when-value\": \"business\",\n\t\"data-fui-cond-disabled\": \"1\",\n}\n",
		},
		oldMarks: []string{"data-when-name", "data-when-value", "data-fui-cond-disabled"},
		newFiles: map[string]string{
			"n10.go": "package app\n\nvar condAttrs = map[string]string{\n\t\"data-hui-when\": \"kind\",\n\t\"data-hui-when-value\": \"business\",\n}\n",
		},
	},
	12: { // lightbox classes
		oldFiles: map[string]string{
			"n12.go":  "package app\n\nvar boxSel = \".ui-lightbox__full[data-fui-zoomed] { z-index: 10 }\"\n",
			"n12.css": ".ui-lightbox__viewer { inset: 0 }\n",
		},
		oldMarks: []string{"ui-lightbox__full", ".ui-lightbox__viewer"},
		newFiles: map[string]string{
			"n12.go": "package app\n\nvar boxSel = \".fui-lightbox__full[data-fui-zoomed] { z-index: 10 }\"\n",
		},
	},
	13: { // display family
		oldFiles: map[string]string{
			"n13.go":  "package app\n\nvar whoCls = \"ui-avatar-group ui-avatar\"\n",
			"n13.css": ".ui-bar-chart__grid { stroke: #eee }\n",
		},
		oldMarks: []string{"ui-avatar-group ui-avatar", ".ui-bar-chart__grid"},
		newFiles: map[string]string{
			"n13.go":  "package app\n\nvar whoCls = \"fui-avatar-group fui-avatar\"\n",
			"n13.css": ".fui-bar-chart__grid { stroke: #eee }\n",
		},
	},
	14: { // shell family
		oldFiles: map[string]string{
			"n14.go":  "package app\n\nvar navCls = \"ui-sidebar__group\"\n",
			"n14.css": ".ui-site-header__inner { max-width: 72rem }\n",
		},
		oldMarks: []string{"ui-sidebar__group", ".ui-site-header__inner"},
		newFiles: map[string]string{
			"n14.go":  "package app\n\nvar navCls = \"fui-sidebar__group\"\n",
			"n14.css": ".fui-site-header__inner { max-width: 72rem }\n",
		},
	},
	15: { // residue family
		oldFiles: map[string]string{
			"n15.go":  "package app\n\nvar tableCls = \"ui-data-table ui-data-table__sort\"\n",
			"n15.css": ".ui-tooltip { position: absolute }\n",
		},
		oldMarks: []string{"ui-data-table ui-data-table__sort", ".ui-tooltip"},
		newFiles: map[string]string{
			"n15.go":  "package app\n\nvar tableCls = \"fui-data-table fui-data-table__sort\"\n",
			"n15.css": ".fui-tooltip { position: absolute }\n",
		},
	},
	17: { // retired kernel attributes
		oldFiles: map[string]string{
			"n17.go": "package app\n\nvar kernelAttrs = map[string]string{\n\t\"data-fui-disclosure-persist\": \"toc\",\n\t\"data-fui-pane-deeplink\":      \"docs\",\n\t\"data-fui-scrollspy\":          \"rail\",\n}\n",
		},
		oldMarks: []string{"data-fui-disclosure-persist", "data-fui-pane-deeplink", "data-fui-scrollspy"},
		newFiles: map[string]string{
			"n17.go": "package app\n\nvar kernelAttrs = map[string]string{\n\t\"data-hui-disclosure-persist\": \"toc\",\n\t\"data-hui-pane-deeplink\":      \"docs\",\n\t\"data-hui-rail\":               \"rail\",\n}\n",
		},
	},
	11: { // upload filename paragraph
		oldFiles: map[string]string{
			"n11.go":  "package app\n\nvar uploadCSS = \".ui-fileupload__filename:empty { display: none }\"\n",
			"n11.css": ".zone .ui-fileupload__filename:empty { display: none }\n",
		},
		oldMarks: []string{`uploadCSS = ".ui-fileupload__filename`, ".zone .ui-fileupload__filename"},
		newFiles: map[string]string{
			"n11.go": "package app\n\nvar hintSuffix = \"-accept\"\n",
		},
	},
	18: { // legacy token aliases
		oldFiles: map[string]string{
			"n18.go":  "package app\n\nvar ringVar = \"color: var(--color-ring)\"\n",
			"n18.css": ":root {\n  --color-muted: #6b7280;\n  --color-warn-soft: rgba(0, 0, 0, .1);\n}\n",
		},
		oldMarks: []string{"--color-ring", "--color-muted", "--color-warn-soft"},
		newFiles: map[string]string{
			"n18.go":  "package app\n\nvar ringVar = \"color: var(--color-warning)\"\n",
			"n18.css": ":root {\n  --color-surface-soft: #6b7280;\n  --color-warning: #b45309;\n}\n",
		},
	},
	19: { // packed auth hidden next input
		oldFiles: map[string]string{
			"n19.go": "package app\n\nvar nextRaw = \"<input type=\\\"hidden\\\" name=\\\"next\\\" value=\\\"/dash\\\">\"\n",
		},
		oldMarks: []string{`name=\"next`},
		newFiles: map[string]string{
			"n19.go": "package app\n\nvar nextType, nextName = \"hidden\", \"next\"\n",
		},
	},
	22: { // infinitescroll hooks and cursor header
		oldFiles: map[string]string{
			"n22.go": "package app\n\nvar feedAttrs = map[string]string{\"data-fui-infinite-scroll\": \"1\"}\n\nvar cursorHdr = \"X-Gofastr-Infinite-Cursor: 9\"\n",
		},
		oldMarks: []string{"data-fui-infinite-scroll", "X-Gofastr-Infinite-Cursor"},
		newFiles: map[string]string{
			"n22.go": "package app\n\nvar pollEverySec = 5\n",
		},
	},
	25: { // multiselect hooks
		oldFiles: map[string]string{
			"n25.go": "package app\n\nvar msAttrs = map[string]string{\n\t\"data-fui-multiselect\":        \"tags\",\n\t\"data-fui-multiselect-remove\": \"3\",\n}\n",
		},
		oldMarks: []string{"data-fui-multiselect", "data-fui-multiselect-remove"},
		newFiles: map[string]string{
			"n25.go": "package app\n\nvar msAttrs = map[string]string{\n\t\"data-hui-multiselect\":        \"tags\",\n\t\"data-hui-multiselect-remove\": \"3\",\n}\n",
		},
	},
	26: { // sortable hooks
		oldFiles: map[string]string{
			"n26.go": "package app\n\nvar sortAttrs = map[string]string{\n\t\"data-fui-sortable\":      \"list\",\n\t\"data-fui-sortable-item\": \"7\",\n}\n",
		},
		oldMarks: []string{"data-fui-sortable", "data-fui-sortable-item"},
		newFiles: map[string]string{
			"n26.go": "package app\n\nvar sortAttrs = map[string]string{\n\t\"data-hui-sortable\":      \"list\",\n\t\"data-hui-sortable-item\": \"7\",\n}\n",
		},
	},
	27: { // tree toggle hook
		oldFiles: map[string]string{
			"n27.go": "package app\n\nvar treeAttrs = map[string]string{\"data-fui-tree-toggle\": \"n4\"}\n",
		},
		oldMarks: []string{"data-fui-tree-toggle"},
		newFiles: map[string]string{
			"n27.go": "package app\n\nvar treeAttrs = map[string]string{\"data-hui-tree-toggle\": \"n4\"}\n",
		},
	},
	36: { // page dimension variables
		oldFiles: map[string]string{
			"n36.go":  "package app\n\nvar wideVar = \"--ui-container-wide: 1240px\"\n",
			"n36.css": ":root { --ui-layout-container-width: 72rem; }\n",
		},
		oldMarks: []string{"--ui-container-wide", "--ui-layout-container-width"},
		newFiles: map[string]string{
			"n36.go":  "package app\n\nvar pageWidth = \"72rem\"\n",
			"n36.css": ":root { --size-page-width: 72rem; }\n",
		},
	},
	39: { // xxl/xxxl size-scale spellings
		oldFiles: map[string]string{
			"n39.go":  "package app\n\nvar scaleGap = \"var(--spacing-xxxl)\"\n\nvar scalePad = \"{spacing.xxl}\"\n\nvar scaleDisp = \"{typography.xxxl}\"\n",
			"n39.css": "--spacing-xxl: 32px;\n--breakpoint-xxl: 64em;\n",
		},
		oldMarks: []string{"--spacing-xxxl", "{spacing.xxl}", "{typography.xxxl}", "--spacing-xxl: 32px", "--breakpoint-xxl: 64em"},
		newFiles: map[string]string{
			"n39.go":  "package app\n\nvar scaleGap = \"var(--spacing-3xl)\"\n\nvar scalePad = \"{spacing.2xl}\"\n",
			"n39.css": "--spacing-2xl: 32px;\n--breakpoint-2xl: 64em;\n",
		},
	},
}

// TestV086StringsOldVsNew builds one pre-migration app and one migrated
// app from every note's string/css pair and scans both with the whole
// v0.86.0 release: each old spelling hits on its line, each migrated
// spelling is silent, and a note with string or css matchers but no
// pair fails.
// The hidden next input in every spelling HTML accepts: attribute
// case, quoting and spacing; and nothing that only resembles it.
func TestV086NextInputSpellings(t *testing.T) {
	_, rel := v086Loaded(t)
	re := rel.Notes[19].Find.Strings.Match
	if re == nil {
		t.Fatal("note 19 lost its strings.match")
	}
	for _, v := range []string{
		`<input type="hidden" name="next" value="/dash">`,
		`<input type=hidden name=next value=/dash>`,
		`<input type='hidden' name='next'>`,
		`<INPUT TYPE="hidden" NAME="next">`,
		`<input type="hidden" name = "next">`,
		"<input\ttype=\"hidden\"\nname=\"next\">",
		`<input value="/dash" name=next>`,
	} {
		if !re.MatchString(v) {
			t.Errorf("missed %q", v)
		}
	}
	for _, v := range []string{
		`<input type="hidden" data-name="next">`,
		`<input type="hidden" name="nextPage">`,
		`<meta name="next">`,
		`<a name="next">`,
		`name="next"`,
	} {
		if re.MatchString(v) {
			t.Errorf("matched %q", v)
		}
	}
}

func TestV086StringsOldVsNew(t *testing.T) {
	reg, rel := v086Loaded(t)
	oldFiles, newFiles := map[string]string{}, map[string]string{}
	for _, pair := range v086StringPairs {
		for k, v := range pair.oldFiles {
			oldFiles[k] = v
		}
		for k, v := range pair.newFiles {
			newFiles[k] = v
		}
	}
	// A stylesheet under a skipped directory must never match: the
	// engine's walk skips node_modules, as the legacy walk did.
	oldFiles["node_modules/pkg/skip.css"] = ".save .ui-button { color: red }\n"
	oldRes := scantest.Run(t, scantest.App(t, oldFiles, scantest.Options{}), rel.Notes, reg.MarkerSinks)
	newRes := scantest.Run(t, scantest.App(t, newFiles, scantest.Options{}), rel.Notes, reg.MarkerSinks)
	for _, n := range rel.Notes {
		for l := range hitLines(oldRes, n) {
			if strings.HasPrefix(l, "node_modules/") {
				t.Errorf("a stylesheet under node_modules matched: %s", l)
			}
		}
	}
	for i, n := range rel.Notes {
		if n.Find.Strings.Empty() && n.Find.CSS.Empty() {
			continue
		}
		pair, ok := v086StringPairs[i]
		if !ok {
			t.Errorf("note %d (%s): carries string/css matchers but has no old/new pair in v086StringPairs", i, n.Change)
			continue
		}
		want := v086MarkLines(t, i, pair.oldFiles, pair.oldMarks)
		got := hitLines(oldRes, n)
		for l := range want {
			if !got[l] {
				t.Errorf("note %d (%s): expected a hit at %s, got hits %v", i, n.Change, l, got)
			}
		}
		if lines := hitLines(newRes, n); len(lines) > 0 {
			t.Errorf("note %d (%s): fires on the migrated spellings at %v", i, n.Change, lines)
		}
	}
}

// v086Kit is a stub of the gofastr module at v0.85.0 holding just the
// symbols the v0.86.0 notes retire, at their real import paths.
var v086Kit = map[string]string{
	"core-ui/style/style.go": `package style

type ThemeRef struct{ Hash string }

type Theme struct{ DarkColors map[string]string }

func DarkSchemeCSS(dark map[string]string) string { return "" }
`,
	"core-ui/html/html.go": `package html

type Attrs map[string]string

type DetailsConfig struct {
	Summary    string
	Disclosure bool
}

type TextConfig struct{ ExtraAttrs Attrs }

func Details(cfg DetailsConfig) string { return "" }

func ContainerType(containerType, name string) Attrs { return nil }

func Span(cfg TextConfig) string { return "" }
`,
	"core-ui/registry/registry.go": `package registry

type Style struct{}

func RegisterStyle(name string, opts ...any) *Style { return nil }

func Lookup(name string) (*Style, bool) { return nil, false }
`,
	"core-ui/widget/preset/preset.go": `package preset

type Builder struct{}

func Drawer(name string) *Builder { return nil }

func ToastStack(name string) string { return "" }

func (b *Builder) Name(name string) *Builder { return b }
`,
	"core-ui/interactive/interactive.go": `package interactive

type SectionMenuConfig struct{ DrawerName string }
`,
	"core-ui/app/app.go": `package app

import "context"

type Layout struct{}

type LayoutSpec struct{}

func NewLayout(name string) *Layout { return nil }

func LayoutBaseCSS() string { return "" }

func (l *Layout) WithHeader(c string) *Layout    { return l }
func (l *Layout) WithSidebar(c string) *Layout   { return l }
func (l *Layout) WithFooter(c string) *Layout    { return l }
func (l *Layout) WithContainer() *Layout         { return l }
func (l *Layout) WithStickyHeader() *Layout      { return l }
func (l *Layout) WithKey(key string) *Layout     { return l }
func (l *Layout) Wrap(content any) any           { return content }
func (l *Layout) WrapCtx(ctx context.Context, content any) any { return content }
`,
	"framework/ui/ui.go": `package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/patterns/pagination"
)

type DataTableConfig struct {
	Pagination      *pagination.Config
	SortHrefPattern string
	IslandSignal    string
	IslandEndpoint  string
}

type ButtonConfig struct {
	ExtraAttrs html.Attrs
	Disabled   bool
}

func Button(cfg ButtonConfig) string { return "" }

type FormConfig struct {
	Action     string
	ID         string
	ExtraAttrs html.Attrs
}

func Form(cfg FormConfig) string { return "" }

type FormFieldConfig struct{ Input string }

func FormField(cfg FormFieldConfig) string { return "" }

type ConditionalFieldConfig struct{ WhenName string }

func ConditionalFieldVisible(cfg ConditionalFieldConfig) string { return "" }

func (cfg ConditionalFieldConfig) EvaluateInitialState(current string) bool { return false }

type SidebarConfig struct {
	DrawerName         string
	CollapseStorageKey string
}

type ControlConfig struct{ ExtraAttrs html.Attrs }

func Control(cfg ControlConfig) string { return "" }

func ToastStackSignal(name string) string { return "" }

type SiteHeaderConfig struct{ Brand string }

func SiteHeader(cfg SiteHeaderConfig) string { return "" }

type SiteFooterConfig struct{}

func SiteFooter(cfg SiteFooterConfig) string { return "" }

type DocLayoutConfig struct{}

func DocLayout(cfg DocLayoutConfig) string { return "" }

type DocPager struct{ Prev, Next string }

func DocPrevNext(pager DocPager) string { return "" }

type DocCrumb struct{ Label, Href string }
`,
	"framework/gallery/gallery.go": `package gallery

func MustLookup(slug string) string { return "" }

func Lookup(slug string) (string, bool) { return "", false }
`,
	"framework/headless/headless.go": `package headless

type ProgressProps struct {
	Label        string
	LabelVisible bool
}

type FieldControl struct{ ID string }
`,
	"core-ui/patterns/accordion/accordion.go": `package accordion

type GroupConfig struct{}

type StackConfig struct{}

type Item struct{ Title string }

func Group(cfg GroupConfig, items ...Item) string { return "" }

func Stack(cfg StackConfig, items ...Item) string { return "" }
`,
	"core-ui/patterns/nestedlist/nestedlist.go": `package nestedlist

type Config struct{ Items []string }

func Render(cfg Config) string { return "" }
`,
	"core-ui/patterns/infinitescroll/infinitescroll.go": `package infinitescroll

type Config struct{ Endpoint string }

func Render(cfg Config) string { return "" }
`,
	"core-ui/patterns/breadcrumbs/breadcrumbs.go": `package breadcrumbs

type Config struct{}

type Crumb struct {
	Text    string
	Href    string
	Current bool
}

func New(cfg Config, crumbs ...Crumb) string { return "" }
`,
	"core-ui/patterns/pagination/pagination.go": `package pagination

type Config struct {
	Total, Current int
	HrefPattern    string
}

func New(cfg Config) string { return "" }
`,
	"core-ui/patterns/progress/progress.go": `package progress

type Config struct{ LabelVisible bool }

func New(cfg Config) string { return "" }
`,
	"core-ui/patterns/multiselect/multiselect.go": `package multiselect

type Config struct{ Name string }

func Render(cfg Config) string { return "" }
`,
	"core-ui/patterns/sortablelist/sortablelist.go": `package sortablelist

type Config struct{ Endpoint string }

func Render(cfg Config) string { return "" }
`,
	"core-ui/patterns/tree/tree.go": `package tree

type Config struct{ SignalPrefix string }

func Render(cfg Config) string { return "" }
`,
}

// v086GoOldFiles is one app written against v0.85.0: every Go API the
// v0.86.0 notes retire, one spelling per note.
var v086GoOldFiles = map[string]string{
	"n40.go": `package app

import "github.com/DonaldMurillo/gofastr/core-ui/patterns/pagination"

var pageCfg = &pagination.Config{Total: 4, Current: 1, HrefPattern: "?p=%d"}

var pager = pagination.New(*pageCfg)
`,
	"n41.go": `package app

import "github.com/DonaldMurillo/gofastr/framework/ui"

var sortTable = ui.DataTableConfig{
	SortHrefPattern: "?sort=%s&dir=%s",
	IslandSignal:    "rows",
	IslandEndpoint:  "/rows",
}
`,
	"n00.go": `package app

import "github.com/DonaldMurillo/gofastr/core-ui/style"

var dark = style.ThemeRef{}

var darkHash = dark.Hash
`,
	"n01.go": `package app

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

var saveBtn = ui.Button(ui.ButtonConfig{ExtraAttrs: html.Attrs{"disabled": ""}})
`,
	"n02.go": `package app

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

var likeBtn = ui.Button(ui.ButtonConfig{ExtraAttrs: html.Attrs{
	"data-fui-toggle-endpoint": "/api/like",
	"data-fui-optimistic-idle": "",
	"data-fui-signal":          "count",
}})
`,
	"n04.go": `package app

import "github.com/DonaldMurillo/gofastr/framework/ui"

var emailField = ui.FormField(ui.FormFieldConfig{Input: "<input type=email>"})
`,
	"n06.go": `package app

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

var filterForm = ui.Form(ui.FormConfig{ExtraAttrs: html.Attrs{
	"data-fui-push-state": "/lists",
	"data-fui-rpc-body":   "{}",
}})
`,
	"n07.go": `package app

import "github.com/DonaldMurillo/gofastr/framework/ui"

var extForm = ui.Form(ui.FormConfig{Action: "//cdn.example.com/save"})
`,
	"n10.go": `package app

import "github.com/DonaldMurillo/gofastr/framework/ui"

var kindCfg = ui.ConditionalFieldConfig{WhenName: "kind"}

var preShow = kindCfg.EvaluateInitialState("business")

var preField = ui.ConditionalFieldVisible(kindCfg)
`,
	"n16.go": `package app

import "github.com/DonaldMurillo/gofastr/core-ui/html"

var faqDetails = html.Details(html.DetailsConfig{Summary: "More", Disclosure: true})
`,
	"n20.go": `package app

import "github.com/DonaldMurillo/gofastr/core-ui/patterns/accordion"

var faq = accordion.Group(accordion.GroupConfig{}, accordion.Item{Title: "Q"})
`,
	"n21.go": `package app

import "github.com/DonaldMurillo/gofastr/core-ui/patterns/nestedlist"

var sitemap = nestedlist.Render(nestedlist.Config{Items: nil})
`,
	"n22.go": `package app

import "github.com/DonaldMurillo/gofastr/core-ui/patterns/infinitescroll"

var feed = infinitescroll.Render(infinitescroll.Config{Endpoint: "/feed"})
`,
	"n23.go": `package app

import "github.com/DonaldMurillo/gofastr/core-ui/patterns/breadcrumbs"

var crumbs = breadcrumbs.New(breadcrumbs.Config{}, breadcrumbs.Crumb{Text: "Tags"})
`,
	"n24.go": `package app

import "github.com/DonaldMurillo/gofastr/core-ui/patterns/progress"

var meter = progress.New(progress.Config{LabelVisible: true})
`,
	"n25.go": `package app

import "github.com/DonaldMurillo/gofastr/core-ui/patterns/multiselect"

var tagPicker = multiselect.Render(multiselect.Config{Name: "tags"})
`,
	"n26.go": `package app

import "github.com/DonaldMurillo/gofastr/core-ui/patterns/sortablelist"

var ranked = sortablelist.Render(sortablelist.Config{Endpoint: "/rank"})
`,
	"n27.go": `package app

import "github.com/DonaldMurillo/gofastr/core-ui/patterns/tree"

var nodes = tree.Config{SignalPrefix: "nodes-"}
`,
	"n28.go": `package app

import "github.com/DonaldMurillo/gofastr/core-ui/app"

var layoutName = "app"

var appLayout = app.NewLayout(layoutName)
`,
	"n29.go": `package app

import "github.com/DonaldMurillo/gofastr/core-ui/app"

func headerLayout(l *app.Layout, hdr string) *app.Layout { return l.WithHeader(hdr) }
`,
	"n30.go": `package app

import "github.com/DonaldMurillo/gofastr/core-ui/app"

func sidebarLayout(l *app.Layout, nav string) *app.Layout { return l.WithSidebar(nav) }
`,
	"n31.go": `package app

import "github.com/DonaldMurillo/gofastr/core-ui/app"

func footerLayout(l *app.Layout, foot string) *app.Layout { return l.WithFooter(foot) }
`,
	"n32.go": `package app

import "github.com/DonaldMurillo/gofastr/core-ui/app"

func containerLayout(l *app.Layout) *app.Layout { return l.WithContainer() }
`,
	"n33.go": `package app

import "github.com/DonaldMurillo/gofastr/core-ui/app"

func stickyLayout(l *app.Layout) *app.Layout { return l.WithStickyHeader() }
`,
	"n34.go": `package app

import "github.com/DonaldMurillo/gofastr/core-ui/app"

var shellCSS = app.LayoutBaseCSS() + "x"
`,
	"n35.go": `package app

import "github.com/DonaldMurillo/gofastr/core-ui/app"

func wrapPage(l *app.Layout, body string) any { return l.Wrap(body) }
`,
	"n37.go": `package app

import "github.com/DonaldMurillo/gofastr/framework/ui"

var siteHdr = ui.SiteHeader(ui.SiteHeaderConfig{Brand: "Acme"})

var siteFtr = ui.SiteFooter(ui.SiteFooterConfig{})

var docs = ui.DocLayout(ui.DocLayoutConfig{})

var docPager = ui.DocPager{}

var docPN = ui.DocPrevNext(docPager)

var docCrumb = ui.DocCrumb{Label: "Tags", Href: "/tags"}
`,
	"n38.go": `package app

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/framework/gallery"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

var cardsGrid = html.ContainerType("inline-size", "cards")

var darkBlock = style.DarkSchemeCSS(nil)

var entryLookup = gallery.MustLookup

var toastSig = ui.ToastStackSignal("app")
`,
}

// v086GoOldMarks holds, per note index, the marks whose lines each
// Go-API note must hit.
var v086GoOldMarks = map[int][]string{
	0:  {"var darkHash = dark.Hash"},
	1:  {`"disabled": ""`},
	2:  {`"data-fui-toggle-endpoint"`, `"data-fui-optimistic-idle"`, `"data-fui-signal"`},
	4:  {"Input:"},
	6:  {`"data-fui-push-state"`, `"data-fui-rpc-body"`},
	7:  {"Action:"},
	10: {"EvaluateInitialState(", "ConditionalFieldVisible("},
	16: {"Disclosure: true"},
	20: {"patterns/accordion"},
	21: {"patterns/nestedlist"},
	22: {"patterns/infinitescroll"},
	23: {"patterns/breadcrumbs"},
	24: {"patterns/progress"},
	25: {"patterns/multiselect"},
	26: {"patterns/sortablelist"},
	27: {"patterns/tree", "SignalPrefix:"},
	28: {"app.NewLayout(layoutName)"},
	29: {"l.WithHeader(hdr)"},
	30: {"l.WithSidebar(nav)"},
	31: {"l.WithFooter(foot)"},
	32: {"l.WithContainer()"},
	33: {"l.WithStickyHeader()"},
	34: {"app.LayoutBaseCSS()"},
	35: {"l.Wrap(body)"},
	37: {"ui.SiteHeader(", "ui.SiteFooter(", "ui.DocLayout(", "ui.DocPager{}", "ui.DocPrevNext(", "ui.DocCrumb{"},
	38: {"html.ContainerType(", "style.DarkSchemeCSS(", "gallery.MustLookup", "ui.ToastStackSignal("},
	40: {"core-ui/patterns/pagination\"", "&pagination.Config{", "pagination.New("},
	41: {"SortHrefPattern:", "IslandSignal:", "IslandEndpoint:"},
}

// v086GoNewFiles is the same app after the migration the notes
// prescribe. The notes whose symbol keeps its name with a new shape
// (ThemeRef.Hash the field becomes a method, FormFieldConfig.Input the
// pre-built markup becomes a builder, app.NewLayout gains arguments)
// have no entry here: their new spelling still references the old
// symbol, so silence against the v0.85.0 stub proves nothing.
var v086GoNewFiles = map[string]string{
	"m40.go": `package app

import "github.com/DonaldMurillo/gofastr/framework/ui"

var pagedTable = ui.DataTableConfig{Pagination: nil}
`,
	"m41.go": `package app

import "github.com/DonaldMurillo/gofastr/framework/ui"

var sortTable = ui.DataTableConfig{}
`,
	"m02.go": `package app

import "github.com/DonaldMurillo/gofastr/framework/ui"

var saveBtn = ui.Button(ui.ButtonConfig{Disabled: true})
`,
	"m03.go": `package app

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

var likeBtn = ui.Button(ui.ButtonConfig{ExtraAttrs: html.Attrs{
	"data-fui-rpc":        "/api/like",
	"data-fui-rpc-signal": "count",
}})
`,
	"m06.go": `package app

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

var filterForm = ui.Form(ui.FormConfig{ExtraAttrs: html.Attrs{
	"data-fui-rpc":        "/save",
	"data-fui-rpc-signal": "saved",
}})
`,
	"m07.go": `package app

import "github.com/DonaldMurillo/gofastr/framework/ui"

var extForm = ui.Form(ui.FormConfig{Action: "/customers"})
`,
	"m10.go": `package app

var watchAttrs = map[string]string{"data-hui-when": "kind", "data-hui-when-value": "business"}
`,
	"m16.go": `package app

import "github.com/DonaldMurillo/gofastr/core-ui/html"

var plainDetails = html.Details(html.DetailsConfig{Summary: "More"})
`,
	"m20.go": `package app

import "github.com/DonaldMurillo/gofastr/framework/ui"

var faq = ui.Button(ui.ButtonConfig{})
`,
	"m29.go": `package app

import "example.com/app/mailer"

var tagged = mailer.WithHeader("X-Tenant", "id")
`,
	"m30.go": `package app

import "example.com/app/uiown"

var framed = uiown.Stack("hdr", "body", "foot")
`,
	"m34.go": `package app

var shellCSS = "x"
`,
	"m35.go": `package app

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"example.com/app/fanout"
)

func page(l *app.Layout, body string) any { return l.WrapCtx(context.Background(), body) }

var sent = fanout.Wrap("node", "body")
`,
	"m37.go": `package app

import "example.com/app/siteheader"

var siteHdr = siteheader.Render(siteheader.Config{Name: "Acme"})
`,
	"m38.go": `package app

import (
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core-ui/widget/preset"
	"github.com/DonaldMurillo/gofastr/framework/gallery"
)

var entry, entryOK = gallery.Lookup("tags")

var darkTheme = style.Theme{DarkColors: nil}

var toast2 = preset.ToastStack("app")
`,
	"mailer/mailer.go":         `package mailer` + "\n" + `func WithHeader(key, value string) string { return "" }` + "\n",
	"uiown/uiown.go":           "package uiown\n\nfunc Stack(children ...string) string { return \"\" }\n",
	"fanout/fanout.go":         "package fanout\n\nfunc Wrap(nodeID, body string) string { return \"\" }\n",
	"siteheader/siteheader.go": "package siteheader\n\ntype Config struct{ Name string }\n\nfunc Render(cfg Config) string { return \"\" }\n",
}

// TestV086GoAPIOldVsNew scans the pre-migration app and the migrated
// app (both against the v0.85.0 stub kit) with the whole release: every
// Go-API note hits its old spelling's line, and every note stays silent
// on the migrated app.
func TestV086GoAPIOldVsNew(t *testing.T) {
	reg, rel := v086Loaded(t)
	oldRes := scantest.Run(t, scantest.App(t, v086GoOldFiles, scantest.Options{Kit: v086Kit}), rel.Notes, reg.MarkerSinks)
	newRes := scantest.Run(t, scantest.App(t, v086GoNewFiles, scantest.Options{Kit: v086Kit}), rel.Notes, reg.MarkerSinks)
	for i, n := range rel.Notes {
		goAPI := len(n.Find.Uses) > 0 || len(n.Find.Imports) > 0 || len(n.Find.Fields) > 0
		marks, ok := v086GoOldMarks[i]
		if goAPI != ok {
			// A Go-API note with no fixture is a matcher nobody watched
			// fire; a fixture with no Go-API find tests nothing.
			t.Errorf("note %d (%s): Go-API find %v, pinned in v086GoOldMarks %v", i, n.Change, goAPI, ok)
			continue
		}
		if !ok {
			continue
		}
		want := v086MarkLines(t, i, v086GoOldFiles, marks)
		got := hitLines(oldRes, n)
		for l := range want {
			if !got[l] {
				t.Errorf("note %d (%s): expected a hit at %s, got hits %v", i, n.Change, l, got)
			}
		}
		if lines := hitLines(newRes, n); len(lines) > 0 {
			t.Errorf("note %d (%s): fires on the migrated spelling at %v", i, n.Change, lines)
		}
	}
	if len(v086GoOldMarks) == 0 {
		t.Fatal("no Go-API notes pinned")
	}
}

// TestV086CurrentVocabularySilent pins spellings that are current
// framework vocabulary, not migration work: stdlib hash types, the
// disabled attr on a non-button component, the live data-fui-signal
// display seam outside buttons, the command palette's live
// data-fui-push-state markup, headless.ProgressProps, and CSS
// selectors that carry a data-fui-comp marker value.
func TestV086CurrentVocabularySilent(t *testing.T) {
	reg, rel := v086Loaded(t)
	files := map[string]string{
		"silent.go": `package app

import (
	"crypto"
	"hash"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/widget/preset"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

var algo crypto.Hash

func sign(h hash.Hash) error { return nil }

var qty = ui.Control(ui.ControlConfig{ExtraAttrs: html.Attrs{"disabled": ""}})

var labCount = html.Span(html.TextConfig{ExtraAttrs: html.Attrs{"id": "lab-count", "data-fui-signal": "lab.count"}})

var paletteItem = "<li role=\"option\" data-fui-push-state=\"/next\"><span>Tags</span></li>"

var upload = headless.ProgressProps{Label: "Upload", LabelVisible: true}

var framedRule = "[data-fui-comp=\"ui-code-block\"].fui-code-block--framed { border: 1px }"

var stackToast = preset.ToastStack("app-toasts")
`,
	}
	res := scantest.Run(t, scantest.App(t, files, scantest.Options{Kit: v086Kit}), rel.Notes, reg.MarkerSinks)
	for i, n := range rel.Notes {
		if lines := hitLines(res, n); len(lines) > 0 {
			t.Errorf("note %d (%s): fires on current framework vocabulary at %v", i, n.Change, lines)
		}
	}
}

// TestV086MarkerSinksAreNames pins the marker sinks: a ui-* name passed
// to the style registry, a drawer name, a data-fui-comp marker value —
// each is an identifier and stays silent, while the same names as
// classes are hits.
func TestV086MarkerSinksAreNames(t *testing.T) {
	reg, rel := v086Loaded(t)
	files := map[string]string{
		"sinks.go": `package app

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/widget/preset"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

const buttonSheet = "ui-button"

var _ = registry.RegisterStyle(buttonSheet)

var _ = registry.RegisterStyle("ui-form")

var _ = registry.Lookup("ui-search-input")

var _ = preset.Drawer("ui-sidebar-drawer").Name("nav")

var _ = ui.SidebarConfig{DrawerName: "ui-sidebar-drawer"}

var _ = interactive.SectionMenuConfig{DrawerName: "ui-sidebar"}

var _ = html.Attrs{"data-fui-comp": "ui-code-block"}

var _ = html.Attrs{"data-fui-open": "ui-sidebar-drawer"}

var buttonClass = "ui-button"

var sidebarClass = "ui-sidebar"
`,
	}
	res := scantest.Run(t, scantest.App(t, files, scantest.Options{Kit: v086Kit}), rel.Notes, reg.MarkerSinks)
	buttonLine := "sinks.go:" + itoa(lineOf(t, files["sinks.go"], `var buttonClass`))
	sidebarLine := "sinks.go:" + itoa(lineOf(t, files["sinks.go"], `var sidebarClass`))
	controls := map[int]string{3: buttonLine, 14: sidebarLine}
	for i, n := range rel.Notes {
		lines := hitLines(res, n)
		want, isControl := controls[i]
		if !isControl {
			if len(lines) > 0 {
				t.Errorf("note %d (%s): a marker-sink name is a hit at %v", i, n.Change, lines)
			}
			continue
		}
		if len(lines) != 1 || !lines[want] {
			t.Errorf("note %d (%s): expected exactly the class control hit at %s, got %v", i, n.Change, want, lines)
		}
	}
}

// TestV086ExamplesStaySilent scans the repo's examples tree (migrated
// code) with every v0.86.0 note's string, css, config and text
// matchers: zero hits. Go-API
// matchers are stripped: a renamed symbol's replacement still exists
// at HEAD and the examples call it, which is correct for an app below
// v0.86.0 and not a property of migrated code.
func TestV086ExamplesStaySilent(t *testing.T) {
	reg, rel := v086Loaded(t)
	notes := make([]*upgrade.Note, len(rel.Notes))
	for i, n := range rel.Notes {
		notes[i] = scantest.Only(n, "strings", "css", "config", "text")
	}
	res := scantest.Run(t, "../../examples", notes, reg.MarkerSinks)
	for i, n := range notes {
		for _, h := range scantest.Hits(res, n) {
			t.Errorf("v0.86.0 note %d (%s): fires on the migrated examples tree: %s", i, n.Change, h)
		}
	}
}
