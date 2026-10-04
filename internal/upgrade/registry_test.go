package upgrade

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// fullDoc exercises every matcher and both note shapes (find and
// nodetect). TestParseFullDocument round-trips
// it into the types; TestParseRefuses corrupts one field of it at a
// time.
const fullDoc = `through: v0.86.0
marker_sinks:
  calls:
    - func: gofastr/core-ui/registry.RegisterStyle
      arg: 0
    - func: gofastr/core-ui/x.Type.Method
      arg: 1
  fields:
    - gofastr/framework/ui.SidebarConfig.DrawerName
  attr_keys: [data-cui-comp]
releases:
  - version: v0.86.0
    title: Headless design system
    notes:
      - change: 'one line'
        breaking: true
        hits: edit
        guidance: one line, actionable
        find:
          uses:
            - gofastr/framework/app.Layout.WithHeader
            - gofastr/framework/ui.SiteHeader
            - database/sql.Open
          shapes:
            - symbol: gofastr/core-ui/app.NewLayout
              type: '^func\(name string\) \*app\.Layout$'
          imports:
            - gofastr/core-ui/patterns/accordion
            - gofastr/core-ui/patterns/...
          fields:
            - field: gofastr/framework/ui.ButtonConfig.ExtraAttrs
              key: disabled
            - field: gofastr/framework/ui.FormConfig.Action
              value: '^(javascript:|//)'
          strings:
            classes: [ui-button]
            attrs: [data-cui-signal, data-cui-toggle-]
            properties: [--color-muted]
            match: 'X-Gofastr-Infinite-Cursor'
          css:
            classes: [ui-button]
            properties: [--color-muted, --spacing-xxl]
          config:
            - key: entities.*.api_prefix
            - key: auth
              value: '^(yes|no|on|off)$'
          gomod:
            go_below: "1.27"
          text:
            - glob: "**/*.js"
              match: 'data-cui-signal'
      - change: 'silent change'
        breaking: true
        guidance: do the thing
        nodetect: no spelling an app carries differs
`

func TestParseFullDocument(t *testing.T) {
	reg, err := Parse(fullDoc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if reg.Through != "v0.86.0" {
		t.Errorf("Through = %q, want v0.86.0", reg.Through)
	}
	if len(reg.Releases) != 1 {
		t.Fatalf("Releases: %d, want 1", len(reg.Releases))
	}

	// Marker sinks, with the gofastr/ shorthand expanded.
	if len(reg.MarkerSinks.Calls) != 2 {
		t.Fatalf("MarkerSinks.Calls: %d, want 2", len(reg.MarkerSinks.Calls))
	}
	wantCall := ParamSink{Func: Symbol{Pkg: ModulePath + "/core-ui/registry", Name: "RegisterStyle"}, Arg: 0}
	if reg.MarkerSinks.Calls[0] != wantCall {
		t.Errorf("Calls[0] = %+v, want %+v", reg.MarkerSinks.Calls[0], wantCall)
	}
	wantMethod := ParamSink{Func: Symbol{Pkg: ModulePath + "/core-ui/x", Name: "Type", Member: "Method"}, Arg: 1}
	if reg.MarkerSinks.Calls[1] != wantMethod {
		t.Errorf("Calls[1] = %+v, want %+v", reg.MarkerSinks.Calls[1], wantMethod)
	}
	wantField := Symbol{Pkg: ModulePath + "/framework/ui", Name: "SidebarConfig", Member: "DrawerName"}
	if len(reg.MarkerSinks.Fields) != 1 || reg.MarkerSinks.Fields[0] != wantField {
		t.Errorf("MarkerSinks.Fields = %+v, want [%+v]", reg.MarkerSinks.Fields, wantField)
	}
	if len(reg.MarkerSinks.AttrKeys) != 1 || reg.MarkerSinks.AttrKeys[0] != "data-cui-comp" {
		t.Errorf("AttrKeys = %v, want [data-cui-comp]", reg.MarkerSinks.AttrKeys)
	}

	rel := reg.Releases[0]
	if rel.Version != "v0.86.0" || rel.Title != "Headless design system" {
		t.Errorf("Releases[0] = %+v", rel)
	}
	if len(rel.Notes) != 2 {
		t.Fatalf("notes: %d, want 2", len(rel.Notes))
	}
	note := rel.Notes[0]
	if note.Change != "one line" || !note.Breaking || note.Guidance != "one line, actionable" {
		t.Errorf("note header = %+v", note)
	}
	if note.Version != "v0.86.0" || note.Line == 0 {
		t.Errorf("note Version/Line = %q/%d, want v0.86.0 and a real line", note.Version, note.Line)
	}

	find := note.Find
	if find.Empty() {
		t.Fatal("find must not be empty")
	}
	uses := []Symbol{
		{Pkg: ModulePath + "/framework/app", Name: "Layout", Member: "WithHeader"},
		{Pkg: ModulePath + "/framework/ui", Name: "SiteHeader"},
		{Pkg: "database/sql", Name: "Open"},
	}
	if len(find.Uses) != 3 {
		t.Fatalf("Uses = %+v, want 3", find.Uses)
	}
	for i, want := range uses {
		if find.Uses[i] != want {
			t.Errorf("Uses[%d] = %+v, want %+v", i, find.Uses[i], want)
		}
	}
	wantShape := ShapeMatch{
		Symbol: Symbol{Pkg: ModulePath + "/core-ui/app", Name: "NewLayout"},
		Type:   regexp.MustCompile(`^func\(name string\) \*app\.Layout$`),
	}
	if len(find.Shapes) != 1 {
		t.Fatalf("Shapes = %+v, want 1", find.Shapes)
	}
	if s := find.Shapes[0]; s.Symbol != wantShape.Symbol || s.Type == nil || s.Type.String() != wantShape.Type.String() {
		t.Errorf("Shapes[0] = %+v, want %+v", s, wantShape)
	}
	wantImports := []string{ModulePath + "/core-ui/patterns/accordion", ModulePath + "/core-ui/patterns/..."}
	if strings.Join(find.Imports, "|") != strings.Join(wantImports, "|") {
		t.Errorf("Imports = %v, want %v", find.Imports, wantImports)
	}
	if len(find.Fields) != 2 {
		t.Fatalf("Fields: %d, want 2", len(find.Fields))
	}
	if f := find.Fields[0]; f.Field != (Symbol{Pkg: ModulePath + "/framework/ui", Name: "ButtonConfig", Member: "ExtraAttrs"}) || f.Key != "disabled" || f.Value != nil {
		t.Errorf("Fields[0] = %+v", f)
	}
	if f := find.Fields[1]; f.Field.Name != "FormConfig" || f.Value == nil || f.Value.String() != `^(javascript:|//)` {
		t.Errorf("Fields[1] = %+v", f)
	}
	s := find.Strings
	if strings.Join(s.Classes, ",") != "ui-button" ||
		strings.Join(s.Attrs, ",") != "data-cui-signal,data-cui-toggle-" ||
		strings.Join(s.Properties, ",") != "--color-muted" ||
		s.Match == nil || s.Match.String() != "X-Gofastr-Infinite-Cursor" {
		t.Errorf("Strings = %+v", s)
	}
	c := find.CSS
	if strings.Join(c.Classes, ",") != "ui-button" || strings.Join(c.Properties, ",") != "--color-muted,--spacing-xxl" {
		t.Errorf("CSS = %+v", c)
	}
	if len(find.Config) != 2 {
		t.Fatalf("Config: %d, want 2", len(find.Config))
	}
	if cm := find.Config[0]; cm.Key != "entities.*.api_prefix" || cm.Value != nil {
		t.Errorf("Config[0] = %+v", cm)
	}
	if cm := find.Config[1]; cm.Key != "auth" || cm.Value == nil || cm.Value.String() != `^(yes|no|on|off)$` {
		t.Errorf("Config[1] = %+v", cm)
	}
	if find.GoMod == nil || find.GoMod.GoBelow != "1.27" {
		t.Errorf("GoMod = %+v, want go_below 1.27", find.GoMod)
	}
	if len(find.Text) != 1 || find.Text[0].Glob != "**/*.js" || find.Text[0].Match == nil || find.Text[0].Match.String() != "data-cui-signal" {
		t.Errorf("Text = %+v", find.Text)
	}

	// The nodetect note: no find, a reason.
	nd := rel.Notes[1]
	if !nd.Find.Empty() || nd.Nodetect == "" {
		t.Errorf("nodetect note = %+v", nd)
	}
}

// hits decides the report section: edit for a breaking note whose
// every hit is dead, review for one whose code may still be right. A
// non-breaking note is always review.
func TestParseHits(t *testing.T) {
	reg, err := Parse(`through: v0.86.0
releases:
  - version: v0.86.0
    notes:
      - change: removed
        breaking: true
        hits: edit
        guidance: g
        find:
          uses: [gofastr/framework/ui.X]
      - change: behaviour
        breaking: true
        hits: review
        guidance: g
        find:
          uses: [gofastr/framework/ui.Y]
      - change: additive
        guidance: g
        find:
          uses: [gofastr/framework/ui.Z]
`)
	if err != nil {
		t.Fatal(err)
	}
	notes := reg.Releases[0].Notes
	for i, want := range []bool{false, true, true} {
		if notes[i].Review != want {
			t.Errorf("note %d (%s): Review = %v, want %v", i, notes[i].Change, notes[i].Review, want)
		}
	}
}

func TestParseRefuses(t *testing.T) {
	note := func(body string) string {
		return "through: v0.86.0\nreleases:\n  - version: v0.86.0\n    notes:\n      - change: c\n        breaking: true\n        guidance: g\n" + body
	}
	cases := []struct {
		name    string
		src     string
		wantErr string
	}{
		{"unknown root key", "through: v0.86.0\nbogus: 1\nreleases: []\n", "unknown key"},
		{"unknown release key", "through: v0.86.0\nreleases:\n  - version: v0.86.0\n    bogus: 1\n", "unknown key"},
		{"unknown note key", note("        bogus: 1\n"), "unknown key"},
		{"unknown find key", note("        find:\n          bogus: 1\n"), "unknown key"},
		{"unknown strings key", note("        find:\n          strings:\n            bogus: []\n"), "unknown key"},
		{"unknown css key", note("        find:\n          css:\n            bogus: []\n"), "unknown key"},
		{"unknown gomod key", note("        find:\n          gomod:\n            bogus: 1\n"), "unknown key"},
		{"unknown fields item key", note("        find:\n          fields:\n            - field: gofastr/framework/ui.C.F\n              bogus: 1\n"), "unknown key"},
		{"unknown config item key", note("        find:\n          config:\n            - key: a\n              bogus: 1\n"), "unknown key"},
		{"unknown text item key", note("        find:\n          text:\n            - glob: '**/*.js'\n              bogus: 1\n"), "unknown key"},
		{"unknown shapes item key", note("        find:\n          shapes:\n            - symbol: gofastr/core-ui/app.NewLayout\n              type: '^x$'\n              bogus: 1\n"), "unknown key"},
		{"unknown marker_sinks key", "through: v0.86.0\nmarker_sinks:\n  bogus: []\nreleases: []\n", "unknown key"},
		{"unknown calls item key", "through: v0.86.0\nmarker_sinks:\n  calls:\n    - func: gofastr/core-ui/registry.RegisterStyle\n      bogus: 1\nreleases: []\n", "unknown key"},

		{"strings match regex", note("        find:\n          strings:\n            match: '[unclosed'\n"), "does not compile"},
		{"field value regex", note("        find:\n          fields:\n            - field: gofastr/framework/ui.C.F\n              value: '[unclosed'\n"), "does not compile"},
		{"text match regex", note("        find:\n          text:\n            - glob: '**/*.js'\n              match: '[unclosed'\n"), "does not compile"},
		{"shapes entry missing type", note("        find:\n          shapes:\n            - symbol: gofastr/core-ui/app.NewLayout\n"), "missing type"},
		{"shapes entry missing symbol", note("        find:\n          shapes:\n            - type: '^x$'\n"), "missing symbol"},
		{"shapes type regex", note("        find:\n          shapes:\n            - symbol: gofastr/core-ui/app.NewLayout\n              type: '[unclosed'\n"), "does not compile"},
		{"config value regex", note("        find:\n          config:\n            - key: a\n              value: '[unclosed'\n"), "does not compile"},

		{"symbol no name", note("        find:\n          uses: [gofastr/framework/ui.]\n"), "malformed symbol"},
		{"symbol package only", note("        find:\n          uses: [gofastr/framework/ui]\n"), "malformed symbol"},
		{"symbol two members", note("        find:\n          uses: [gofastr/framework/ui.A.B.C]\n"), "malformed symbol"},
		{"symbol whitespace", note("        find:\n          uses: ['gofastr/framework/ui. X']\n"), "malformed symbol"},
		{"field member without type", note("        find:\n          fields:\n            - field: gofastr/framework/ui.SiteHeader\n"), "must name Type.Member"},
		{"strings match regex empty", note("        find:\n          strings:\n            match: ''\n"), "is empty"},
		{"breaking not a bool", "through: v0.86.0\nreleases:\n  - version: v0.86.0\n    notes:\n      - change: c\n        guidance: g\n        breaking: maybe\n", "true or false"},
		{"find present but empty", note("        find:\n"), "find is empty"},
		{"find with nodetect", note("        nodetect: reason\n        find:\n          uses: [gofastr/framework/ui.X]\n"), "find and nodetect"},
		{"legacy detect key", note("        detect: 'x'\n"), `unknown key "detect"`},
		{"neither find nor nodetect", note(""), "neither find nor nodetect"},
		{"change spells breaking", "through: v0.86.0\nreleases:\n  - version: v0.86.0\n    notes:\n      - change: 'Breaking: x'\n        breaking: true\n        guidance: g\n        nodetect: r\n", "breaking: true says so"},
		{"breaking find without hits", note("        find:\n          uses: [gofastr/framework/ui.X]\n"), "hits: edit"},
		{"hits not edit or review", note("        hits: maybe\n        find:\n          uses: [gofastr/framework/ui.X]\n"), "edit or review"},
		{"hits on a nodetect note", note("        hits: edit\n        nodetect: reason\n"), "only a breaking note with find"},
		{"hits on a non-breaking note", "through: v0.86.0\nreleases:\n  - version: v0.86.0\n    notes:\n      - change: c\n        guidance: g\n        hits: review\n        find:\n          uses: [gofastr/framework/ui.X]\n", "only a breaking note with find"},

		{"config key empty", note("        find:\n          config:\n            - key: \"\"\n"), "config key is empty"},
		{"config key empty element", note("        find:\n          config:\n            - key: a..b\n"), "empty path element"},
		{"config key leading dot", note("        find:\n          config:\n            - key: .a\n"), "empty path element"},

		{"gomod not a go version", note("        find:\n          gomod:\n            go_below: 1.x\n"), "not a Go version"},
		{"gomod empty", note("        find:\n          gomod:\n            go_below: \"\"\n"), "not a Go version"},

		{"text glob empty", note("        find:\n          text:\n            - glob: \"\"\n              match: x\n"), "glob is empty"},
		{"text glob bad pattern", note("        find:\n          text:\n            - glob: 'docs/[a-.md'\n              match: x\n"), "syntax error in pattern"},
		{"text glob ** in a segment", note("        find:\n          text:\n            - glob: 'docs/a**/x.md'\n              match: x\n"), "\"**\" must be a whole path segment"},
		{"fields key and value", note("        find:\n          fields:\n            - field: example.com/kit/ui.ButtonConfig.ExtraAttrs\n              key: disabled\n              value: x\n"), "at most one of key, value, refused"},
		{"fields value and refused", note("        find:\n          fields:\n            - field: example.com/kit/ui.FormConfig.Action\n              value: x\n              refused: anchor\n"), "at most one of key, value, refused"},
		{"config value and refused", note("        find:\n          config:\n            - key: a.b\n              value: x\n              refused: anchor\n"), "value or refused, not both"},
		{"config refused unknown policy", note("        find:\n          config:\n            - key: a.b\n              refused: links\n"), "refused must be anchor, resource or image_source"},
		{"fields refused unknown policy", note("        find:\n          fields:\n            - field: example.com/kit/ui.FormConfig.Action\n              refused: links\n"), "refused must be anchor, resource or image_source"},
		{"text glob targets go", note("        find:\n          text:\n            - glob: '**/*.go'\n              match: x\n"), "structural matcher"},
		{"text glob targets css", note("        find:\n          text:\n            - glob: '*.css'\n              match: x\n"), "structural matcher"},

		{"sink arg negative", "through: v0.86.0\nmarker_sinks:\n  calls:\n    - func: gofastr/core-ui/registry.RegisterStyle\n      arg: -1\nreleases: []\n", "arg is negative"},
		{"sink arg not an int", "through: v0.86.0\nmarker_sinks:\n  calls:\n    - func: gofastr/core-ui/registry.RegisterStyle\n      arg: zero\nreleases: []\n", "arg must be an integer"},

		{"through not semver", "through: v0.86\nreleases: []\n", "must look like"},
		{"releases missing", "through: v0.86.0\n", "missing releases list"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.src)
			if err == nil {
				t.Fatalf("Parse accepted the document, want an error mentioning %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not mention %q", err.Error(), tc.wantErr)
			}
			if !strings.HasPrefix(err.Error(), "registry:") {
				t.Errorf("error %q must carry the registry: prefix", err.Error())
			}
		})
	}
}

func TestParseErrorsCarryLine(t *testing.T) {
	_, err := Parse("through: v0.86.0\nreleases:\n  - version: v0.86.0\n    notes:\n      - change: c\n        bogus: 1\n")
	if err == nil {
		t.Fatal("Parse accepted an unknown note key")
	}
	re := regexp.MustCompile(`^registry:(\d+):`)
	m := re.FindStringSubmatch(err.Error())
	if m == nil {
		t.Fatalf("error %q has no registry:<line>: prefix", err.Error())
	}
	if m[1] != "6" {
		t.Errorf("error reports line %s, want 6 (the bogus key's line)", m[1])
	}
}

func TestEmbeddedRegistryParsesSorted(t *testing.T) {
	reg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(reg.Releases) < 5 {
		t.Fatalf("registry suspiciously small: %d releases", len(reg.Releases))
	}
	for i, r := range reg.Releases {
		if err := ValidateSemver(r.Version); err != nil {
			t.Errorf("release %d version %q: %v", i, r.Version, err)
		}
		if len(r.Notes) == 0 {
			t.Errorf("release %s has no notes", r.Version)
		}
		for _, n := range r.Notes {
			if n.Change == "" || n.Guidance == "" {
				t.Errorf("release %s: note missing change/guidance: %+v", r.Version, n)
			}
			if n.Version != r.Version {
				t.Errorf("note %q carries Version %q, want %q", n.Change, n.Version, r.Version)
			}
			if n.Line == 0 {
				t.Errorf("note %q carries no registry line", n.Change)
			}
		}
		if i > 0 && !SemverLess(reg.Releases[i-1].Version, r.Version) {
			t.Errorf("registry not sorted ascending: %s before %s", reg.Releases[i-1].Version, r.Version)
		}
	}
}

// TestThroughCoversNewestEntry: `through` is the newest SHIPPED release.
// One file may sit above it: the pending notes for the release being
// built, added in the PR that lands each breaking change (the CHANGELOG
// policy, where entries accrue under Unreleased). Two files above it
// mean a release PR shipped without bumping `through`.
func TestThroughCoversNewestEntry(t *testing.T) {
	reg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := ValidateSemver(reg.Through); err != nil {
		t.Fatalf("through: %v", err)
	}
	var pending []string
	for _, rel := range reg.Releases {
		if SemverLess(reg.Through, rel.Version) {
			pending = append(pending, rel.Version)
		}
	}
	if len(pending) > 1 {
		t.Errorf("through %s is older than %d entries %v: only the next release's pending file may sit above it", reg.Through, len(pending), pending)
	}
	// The pending file names the release that comes next: the patch,
	// minor or major right after through. A file further out stays the
	// single file above through across releases, so nothing else would
	// ever say its version is wrong.
	for _, v := range pending {
		if !slices.Contains(nextVersions(reg.Through), v) {
			t.Errorf("pending releases/%s.yml is not the release after through %s (one of %v)", v, reg.Through, nextVersions(reg.Through))
		}
	}
}

// nextVersions lists the three releases that can follow v: its next
// patch, next minor, and next major.
func nextVersions(v string) []string {
	var major, minor, patch int
	if _, err := fmt.Sscanf(v, "v%d.%d.%d", &major, &minor, &patch); err != nil {
		return nil
	}
	return []string{
		fmt.Sprintf("v%d.%d.%d", major, minor, patch+1),
		fmt.Sprintf("v%d.%d.0", major, minor+1),
		fmt.Sprintf("v%d.0.0", major+1),
	}
}

// TestShippedEntriesAreChangelogReleases: every registry file at or
// below `through` names a release CHANGELOG.md has a heading for. A
// pending file written as v0.87.0 while the release shipped as v0.88.0
// slips under `through` at the bump with a version that never existed;
// this is the tripwire that says rename it.
func TestShippedEntriesAreChangelogReleases(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "CHANGELOG.md"))
	if err != nil {
		t.Fatalf("read CHANGELOG.md: %v", err)
	}
	shipped := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^## \[(\d+\.\d+\.\d+)\]`).FindAllStringSubmatch(string(body), -1) {
		shipped["v"+m[1]] = true
	}
	reg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, rel := range reg.Releases {
		if SemverLess(reg.Through, rel.Version) {
			continue // pending: the release PR names it
		}
		if !shipped[rel.Version] {
			t.Errorf("releases/%s.yml is at or below through %s but CHANGELOG.md has no [%s] release: rename the pending file to the version that shipped", rel.Version, reg.Through, strings.TrimPrefix(rel.Version, "v"))
		}
	}
}

// TestThroughMatchesChangelog is the maintenance tripwire: every
// release PR bumps CHANGELOG.md, and the registry's `through` marker
// must move with it, otherwise `gofastr upgrade` wrongly warns (or
// worse, wrongly reassures) about registry coverage.
func TestThroughMatchesChangelog(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "CHANGELOG.md"))
	if err != nil {
		t.Fatalf("read CHANGELOG.md: %v", err)
	}
	re := regexp.MustCompile(`(?m)^## \[(\d+\.\d+\.\d+)\]`)
	m := re.FindStringSubmatch(string(body))
	if m == nil {
		t.Fatal("no release heading found in CHANGELOG.md")
	}
	reg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if want := "v" + m[1]; reg.Through != want {
		t.Errorf("registry.yml through=%s but CHANGELOG's latest release is %s — bump `through` in the release PR", reg.Through, want)
	}
}

// TestNewestReleaseBreakingNotesCarryFind: a breaking note in the
// release being shipped must carry a Find (so `gofastr upgrade` can
// point at the lines it affects) or a one-line `nodetect` reason (a
// change with no spelling an app carries that differs). Scoped to the
// newest release deliberately: older entries are grandfathered.
func TestNewestReleaseBreakingNotesCarryFind(t *testing.T) {
	reg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	var newest *Release
	for i := range reg.Releases {
		if reg.Releases[i].Version == reg.Through {
			newest = &reg.Releases[i]
		}
	}
	if newest == nil {
		t.Fatalf("no release matching through=%s", reg.Through)
	}
	for _, note := range newest.Notes {
		if !note.Breaking {
			continue
		}
		hasFind := !note.Find.Empty()
		reason := strings.TrimSpace(note.Nodetect)
		switch {
		case hasFind && reason != "":
			t.Errorf("%s: breaking note %q carries both find and nodetect — pick one", newest.Version, note.Change)
		case !hasFind && reason == "":
			t.Errorf("%s: breaking note %q has no find — `gofastr upgrade` cannot show the user where it bites, and no nodetect reason says why", newest.Version, note.Change)
		}
	}
}

func TestParseFieldRefused(t *testing.T) {
	src := "through: v0.86.0\nreleases:\n  - version: v0.86.0\n    notes:\n      - change: c\n        breaking: true\n        hits: edit\n        guidance: g\n        find:\n          fields:\n            - field: example.com/kit/ui.FormConfig.Action\n              refused: anchor\n"
	reg, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	fm := reg.Releases[0].Notes[0].Find.Fields[0]
	if fm.Refused != "anchor" || fm.Key != "" || fm.Value != nil {
		t.Fatalf("field match = %+v, want Refused anchor alone", fm)
	}
}
