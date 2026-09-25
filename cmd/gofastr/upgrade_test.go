package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestUpgradeRegistryParsesAndIsSorted(t *testing.T) {
	reg, err := loadUpgradeRegistry()
	if err != nil {
		t.Fatalf("loadUpgradeRegistry: %v", err)
	}
	if len(reg) < 5 {
		t.Fatalf("registry suspiciously small: %d releases", len(reg))
	}
	for i, r := range reg {
		if _, err := parseSemver(r.Version); err != nil {
			t.Errorf("release %d version %q: %v", i, r.Version, err)
		}
		if len(r.Notes) == 0 {
			t.Errorf("release %s has no notes", r.Version)
		}
		for _, n := range r.Notes {
			if n.Change == "" || n.Guidance == "" {
				t.Errorf("release %s: note missing change/guidance: %+v", r.Version, n)
			}
		}
		if i > 0 && !semverLess(reg[i-1].Version, r.Version) {
			t.Errorf("registry not sorted ascending: %s before %s", reg[i-1].Version, r.Version)
		}
	}
}

func TestSemverLess(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"v0.3.0", "v0.4.0", true},
		{"v0.4.0", "v0.3.0", false},
		{"v0.9.0", "v0.10.0", true},
		{"v0.23.0", "v0.23.0", false},
		{"v0.23.0", "v0.23.1", true},
		{"v1.0.0", "v0.25.0", false},
	}
	for _, c := range cases {
		if got := semverLess(c.a, c.b); got != c.want {
			t.Errorf("semverLess(%s, %s) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestUpgradeNotesInRange(t *testing.T) {
	reg := []upgradeRelease{
		{Version: "v0.3.0"}, {Version: "v0.5.0"}, {Version: "v0.21.0"}, {Version: "v0.23.0"},
	}
	got := releasesInRange(reg, "v0.5.0", "v0.23.0")
	if len(got) != 2 || got[0].Version != "v0.21.0" || got[1].Version != "v0.23.0" {
		t.Errorf("expected (v0.5.0, v0.23.0] = [v0.21.0 v0.23.0], got %+v", got)
	}
	// current == target → empty.
	if got := releasesInRange(reg, "v0.23.0", "v0.23.0"); len(got) != 0 {
		t.Errorf("same-version range must be empty, got %+v", got)
	}
	// Unknown current (older than everything) includes all up to target.
	if got := releasesInRange(reg, "", "v0.5.0"); len(got) != 2 {
		t.Errorf("empty current means from-the-beginning, got %+v", got)
	}
}

func TestGoModGofastrVersion(t *testing.T) {
	dir := t.TempDir()
	writeUpgradeFixture(t, dir, "go.mod", `module example.com/app

go 1.26

require (
	github.com/DonaldMurillo/gofastr v0.21.0
	github.com/other/dep v1.2.3
)
`)
	v, replaced, err := goModGofastrVersion(dir)
	if err != nil {
		t.Fatalf("goModGofastrVersion: %v", err)
	}
	if v != "v0.21.0" || replaced {
		t.Errorf("got v=%q replaced=%v", v, replaced)
	}
}

func TestGoModGofastrVersionReplaceDirective(t *testing.T) {
	dir := t.TempDir()
	writeUpgradeFixture(t, dir, "go.mod", `module example.com/app

go 1.26

require github.com/DonaldMurillo/gofastr v0.21.0

replace github.com/DonaldMurillo/gofastr => ../gofastr
`)
	v, replaced, err := goModGofastrVersion(dir)
	if err != nil {
		t.Fatalf("goModGofastrVersion: %v", err)
	}
	if v != "v0.21.0" || !replaced {
		t.Errorf("got v=%q replaced=%v, want v0.21.0 + replaced", v, replaced)
	}
}

func TestUpgradeDetectorsPointAtLines(t *testing.T) {
	dir := t.TempDir()
	writeUpgradeFixture(t, dir, "theme/theme.go", `package theme

import "github.com/DonaldMurillo/gofastr/framework/ui/theme"

func T() any { return theme.Default(theme.Overrides{}) }
`)
	rel := upgradeRelease{Version: "v0.23.0", Notes: []upgradeNote{{
		Change: "theme.Default() is now adaptive", Breaking: true,
		Guidance: "…", Detect: `theme\.Default\(`,
	}}}
	report := formatUpgradeNotes(dir, []upgradeRelease{rel})
	if !strings.Contains(report, "v0.23.0") || !strings.Contains(report, "theme.Default() is now adaptive") {
		t.Errorf("report missing release/note, got:\n%s", report)
	}
	if !strings.Contains(report, filepath.Join("theme", "theme.go")+":5") {
		t.Errorf("report must point at the detected line, got:\n%s", report)
	}
}

func TestParseUpgradeArgsForms(t *testing.T) {
	for _, args := range [][]string{{"--to", "v0.23.0"}, {"--to=v0.23.0"}} {
		opts, bad := parseUpgradeArgs(args)
		if bad != "" || opts.to != "v0.23.0" {
			t.Errorf("args %v: to=%q bad=%q", args, opts.to, bad)
		}
	}
	opts, bad := parseUpgradeArgs([]string{"./app", "--apply"})
	if bad != "" || opts.root != "./app" || !opts.apply {
		t.Errorf("got %+v bad=%q", opts, bad)
	}
	if _, bad := parseUpgradeArgs([]string{"--wat"}); bad != "--wat" {
		t.Errorf("unknown flag must be reported, got %q", bad)
	}
}

func writeUpgradeFixture(t *testing.T, dir, rel, body string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestUpgradeRegistryThroughCoversNewestEntry(t *testing.T) {
	reg, through, err := loadUpgradeRegistryFull()
	if err != nil {
		t.Fatalf("loadUpgradeRegistryFull: %v", err)
	}
	if _, err := parseSemver(through); err != nil {
		t.Fatalf("through: %v", err)
	}
	if last := reg[len(reg)-1].Version; semverLess(through, last) {
		t.Errorf("through %s is older than the newest entry %s", through, last)
	}
}

// TestUpgradeRegistryThroughMatchesChangelog is the maintenance
// tripwire: every release PR bumps CHANGELOG.md, and the registry's
// `through` marker must move with it, otherwise `gofastr upgrade`
// wrongly warns (or worse, wrongly reassures) about registry coverage.
func TestUpgradeRegistryThroughMatchesChangelog(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "CHANGELOG.md"))
	if err != nil {
		t.Fatalf("read CHANGELOG.md: %v", err)
	}
	re := regexp.MustCompile(`(?m)^## \[(\d+\.\d+\.\d+)\]`)
	m := re.FindStringSubmatch(string(body))
	if m == nil {
		t.Fatal("no release heading found in CHANGELOG.md")
	}
	_, through, err := loadUpgradeRegistryFull()
	if err != nil {
		t.Fatal(err)
	}
	if want := "v" + m[1]; through != want {
		t.Errorf("upgrades.yml through=%s but CHANGELOG's latest release is %s — bump `through` in the release PR", through, want)
	}
}

func TestUpgradeRegistryDetectorsCompile(t *testing.T) {
	reg, err := loadUpgradeRegistry()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range reg {
		for _, n := range r.Notes {
			if n.Detect == "" {
				continue
			}
			if _, err := regexp.Compile(n.Detect); err != nil {
				t.Errorf("%s %q: detect regex does not compile: %v", r.Version, n.Change, err)
			}
		}
	}
}

func TestGoModGofastrVersionBlockReplace(t *testing.T) {
	dir := t.TempDir()
	writeUpgradeFixture(t, dir, "go.mod", `module example.com/app

go 1.26

require github.com/DonaldMurillo/gofastr v0.21.0

replace (
	github.com/DonaldMurillo/gofastr => ../gofastr
	github.com/other/dep => ../dep
)
`)
	v, replaced, err := goModGofastrVersion(dir)
	if err != nil {
		t.Fatalf("goModGofastrVersion: %v", err)
	}
	if v != "v0.21.0" || !replaced {
		t.Errorf("block-form replace must be detected: v=%q replaced=%v", v, replaced)
	}
}

func TestSemverPrereleaseAndPseudoVersions(t *testing.T) {
	// A pseudo-version sits between its base's predecessor and the base.
	if !semverLess("v0.25.0", "v0.25.1-0.20260715120000-abcdef123456") {
		t.Errorf("pseudo-version of v0.25.1 must be newer than v0.25.0")
	}
	if !semverLess("v0.25.1-0.20260715120000-abcdef123456", "v0.25.1") {
		t.Errorf("prerelease must sort before its release")
	}
	if semverLess("v0.25.1", "v0.25.1-0.20260715120000-abcdef123456") {
		t.Errorf("release must not sort before its own prerelease")
	}
	// Prerelease targets parse.
	if _, err := parseSemver("v0.26.0-rc.1"); err != nil {
		t.Errorf("prerelease target must parse: %v", err)
	}
	// releasesInRange with a pseudo-version current skips already-crossed releases.
	reg := []upgradeRelease{{Version: "v0.23.0"}, {Version: "v0.25.0"}}
	got := releasesInRange(reg, "v0.24.1-0.20260701000000-aaaaaaaaaaaa", "v0.25.0")
	if len(got) != 1 || got[0].Version != "v0.25.0" {
		t.Errorf("pseudo-version current must not re-include older notes, got %+v", got)
	}
}

// A breaking note in the release being shipped must carry a detector.
// `gofastr upgrade` uses it to point at the exact lines the release will
// break; without one the note is advice the tool cannot locate for you.
// The one sanctioned exception is a change with no line-level spelling
// an old app carries (a field that survives under the same name on the
// config that changed) — such a note must say why in `nodetect`, so an
// omission is a documented decision, never an oversight.
//
// Scoped to the newest release deliberately. Older entries are
// grandfathered: several describe changes no regex can find (a removed
// CLI subcommand, a default that flipped from allow to deny), and
// backfilling them would mean inventing detectors that match nothing.
// The rule that matters is that each new release ships complete.
func TestNewestReleaseBreakingNotesHaveDetectors(t *testing.T) {
	reg, through, err := loadUpgradeRegistryFull()
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	var newest *upgradeRelease
	for i := range reg {
		if reg[i].Version == through {
			newest = &reg[i]
		}
	}
	if newest == nil {
		t.Fatalf("no release matching through=%s", through)
	}
	for _, note := range newest.Notes {
		if !note.Breaking {
			continue
		}
		hasDetect := strings.TrimSpace(note.Detect) != ""
		reason := strings.TrimSpace(note.Nodetect)
		switch {
		case hasDetect && reason != "":
			t.Errorf("%s: breaking note %q carries both detect and nodetect — pick one", newest.Version, note.Change)
		case !hasDetect && reason == "":
			t.Errorf("%s: breaking note %q has no detect regex — `gofastr upgrade` cannot show the user where it bites, and no nodetect reason says why", newest.Version, note.Change)
		}
	}
}

// The registry writes "BREAKING: …" into the change line and the renderer
// prefixes "! BREAKING:" of its own, so every breaking note rendered as
// "! BREAKING: BREAKING: …" from the first one onward.
func TestBreakingMarkerIsNotDoubled(t *testing.T) {
	cases := map[string]string{
		"BREAKING: the module requires Go 1.27": " the module requires Go 1.27",
		"breaking: lowercase form":              " lowercase form",
		"BREAKING:no space":                     "no space",
		"a plain change line":                   "a plain change line",
		"the word BREAKING: mid-sentence":       "the word BREAKING: mid-sentence",
	}
	for in, want := range cases {
		if got := trimBreakingPrefix(in); got != want {
			t.Errorf("trimBreakingPrefix(%q) = %q, want %q", in, got, want)
		}
	}
}

// v086DetectPairs pins every v0.86.0 detector to one line an app written
// against v0.85.0 carries (the detector must flag it) and the same line
// after the migration the note's guidance prescribes (the detector must
// stay silent — a detect that fires on the new spelling cries wolf on
// every migrated project). Keyed by the note's detect regex.
var v086DetectPairs = map[string][2]string{
	`\.Hash([^(\w]|$)`: {
		`if Dark.Hash != want {`,
		`if Dark.Hash() != want {`,
	},
	`ExtraAttrs[^}]*"disabled"`: {
		`ExtraAttrs: map[string]string{"disabled": ""},`,
		`Disabled: true,`,
	},
	`"data-fui-signal":|"data-fui-toggle-|"data-fui-optimistic-`: {
		`"data-fui-toggle-on": "done",`,
		`Action: interactive.Post("/api/todos").Attrs(),`,
	},
	`(^|[^a-z])ui-button`: {
		`cls := "ui-button ui-button--primary"`,
		`cls := "fui-button fui-button--primary"`,
	},
	`(^|[^a-z])ui-(form|form-field|form-section|select|input-group|validation-summary)`: {
		`cls := "ui-form ui-form--block-actions"`,
		`cls := "fui-form fui-form--block-actions"`,
	},
	`data-fui-rpc-after-text|data-fui-rpc-after-disable|data-fui-rpc-scroll-to|data-fui-push-state`: {
		`"data-fui-rpc-scroll-to": "form",`,
		`ExtraAttrs: interactive.Post("/save").Attrs(),`,
	},
	`Action: *"(javascript:|//)`: {
		`Action: "//cdn.example.com/save",`,
		`Action: "/customers",`,
	},
	`(^|[^a-z])ui-(fileupload|dropzone|conditional-field|textarea|search-input)|data-fui-fileupload`: {
		`cls := "ui-fileupload"`,
		`cls := "fui-upload"`,
	},
	`ConditionalFieldVisible|EvaluateInitialState|data-when-name|data-when-value|data-fui-cond-disabled`: {
		`Visible: ui.ConditionalFieldVisible{},`,
		`attrs := map[string]string{"data-hui-when": "kind", "data-hui-when-value": "business"}`,
	},
	`ui-fileupload__filename`: {
		`sel := ".ui-fileupload__filename:empty"`,
		`hintID := id + "-accept"`,
	},
	`(^|[^a-z])ui-lightbox`: {
		`sel := ".ui-lightbox__full[data-fui-zoomed]"`,
		`sel := ".fui-lightbox__full[data-fui-zoomed]"`,
	},
	`(^|[^a-z])ui-(bar-chart|line-chart|pie-chart|sparkline|optimized-image|pipeline-image|image|gallery|code-block|code-tabs|markdown|terminal-block|terminal-ok|terminal-out|avatar|avatar-group|icon|color-picker|diff-viewer|metric-band|record-summary|pricing-card|auth-card|sign-out|optimistic-action|toggle-action)`: {
		`sel := ".ui-avatar-group .ui-avatar"`,
		`sel := ".fui-avatar-group .fui-avatar"`,
	},
	`(^|[^a-z])ui-(hero|site-header|site-footer|doc-layout|doc-prev-next|workbench|toolbar|filter-toolbar|sidebar|responsive|themed)`: {
		`sel := ".ui-sidebar__group"`,
		`sel := ".fui-sidebar__group"`,
	},
	`(^|[^a-z])ui-(data-table|segmented|cmd-palette|json-viewer|polling-indicator|shortcut-hint|confirm-action|tooltip|visually-hidden)`: {
		`sel := "th a.ui-data-table__sort"`,
		`sel := "th a.fui-data-table__sort"`,
	},
	`Disclosure:`: {
		`out := html.Details(html.DetailsConfig{Summary: "More", Disclosure: true})`,
		`out := ui.Collapsible(ui.CollapsibleConfig{Label: "More"})`,
	},
	`data-fui-disclosure|data-fui-pane-deeplink|data-fui-scrollspy`: {
		`attrs := map[string]string{"data-fui-disclosure-persist": "toc"}`,
		`attrs := map[string]string{"data-hui-disclosure-persist": "toc"}`,
	},
	`--color-(muted|surface-hover|border-subtle|border-hover|primary-hover|primary-foreground|ring)|--color-warn([^a-zA-Z0-9]|$)|--color-warn-soft|--color-warn-strong`: {
		`body := "--color-muted: #6b7280;"`,
		`body := "--color-surface-soft: #6b7280;"`,
	},
	`name=\\?"next`: {
		`next := "<input type=\"hidden\" name=\"next\" value=\"/dash\">"`,
		`next := html.Input(html.InputConfig{Type: "hidden", Name: "next", Value: "/dash"})`,
	},
	`patterns/accordion|accordion\.(Group|Stack)\(`: {
		`faq := accordion.Group(accordion.GroupConfig{}, items...)`,
		`faq := ui.Collapsible(ui.CollapsibleConfig{Name: "faq", Label: q, Children: a})`,
	},
	`patterns/nestedlist|nestedlist\.Render\(|(^|[^a-z])nested-list`: {
		`tree := nestedlist.Render(nestedlist.Config{Items: nodes})`,
		`tree := ui.Tree(ui.TreeConfig{Items: items})`,
	},
	`patterns/infinitescroll|infinitescroll\.Render\(|data-fui-infinite-|X-Gofastr-Infinite-Cursor`: {
		`feed := infinitescroll.Render(infinitescroll.Config{Endpoint: ep})`,
		`w.Poll(5 * time.Second)`,
	},
	`patterns/breadcrumbs|breadcrumbs\.New\(`: {
		`crumbs := breadcrumbs.New(breadcrumbs.Config{}, breadcrumbs.Crumb{Text: "Tags"})`,
		`crumbs := ui.Breadcrumbs(ui.BreadcrumbsConfig{}, ui.Crumb{Text: "Tags"})`,
	},
	`patterns/progress|progress\.New\(|LabelVisible`: {
		`bar := progress.New(progress.Config{Value: 3, Max: 5, LabelVisible: true})`,
		`bar := ui.Progress(ui.ProgressConfig{Value: 3, Max: 5, ShowLabel: true})`,
	},
	`patterns/multiselect|multiselect\.Render\(|data-fui-multiselect`: {
		`ms := multiselect.Render(multiselect.Config{Name: "tags"})`,
		`ms := ui.MultiSelect(ui.MultiSelectConfig{Name: "tags"})`,
	},
	`patterns/sortablelist|sortablelist\.Render\(|data-fui-sortable`: {
		`list := sortablelist.Render(sortablelist.Config{Endpoint: ep, Items: items})`,
		`list := ui.SortableList(ui.SortableListConfig{Endpoint: ep, Items: items})`,
	},
	`patterns/tree|tree\.Render\(|\bSignalPrefix\b|data-fui-tree-toggle`: {
		`t := tree.Render(tree.Config{SignalPrefix: "nodes-"})`,
		`t := ui.Tree(ui.TreeConfig{LazySignalPrefix: "nodes-"})`,
	},
	`\bhtml\.ContainerType\(|\bstyle\.DarkSchemeCSS\(|\bgallery\.MustLookup\(|\bui\.ToastStackSignal\(`: {
		`grid := html.Div(html.ContainerType("inline-size", "cards"), cards)`,
		`grid := html.Div(html.Class("cards"), cards)`,
	},
	`--(spacing|text|breakpoint)-x{2,3}l\b|\{(spacing|typography|text|breakpoints?)\.x{2,3}l\}`: {
		`css := "--spacing-xxl: 32px; gap: var(--spacing-xxxl); padding: {spacing.xxl};"`,
		`css := "--spacing-2xl: 32px; gap: var(--spacing-3xl); padding: {spacing.2xl};"`,
	},
}

// TestV086DetectorsSeparateOldFromNew runs every v0.86.0 detector through
// the real detectHits path (temp project, per-line matching) against one
// pre-stack line and its migrated spelling, and fails when a detector is
// missing its pair — a note whose detect has no pair here is unproven.
func TestV086DetectorsSeparateOldFromNew(t *testing.T) {
	const version = "v0.86.0"
	reg, err := loadUpgradeRegistry()
	if err != nil {
		t.Fatalf("loadUpgradeRegistry: %v", err)
	}
	var rel *upgradeRelease
	for i := range reg {
		if reg[i].Version == version {
			rel = &reg[i]
		}
	}
	if rel == nil {
		t.Fatalf("no %s entry in the registry", version)
	}
	oldDir, newDir := t.TempDir(), t.TempDir()
	seen := map[string]bool{}
	for i, note := range rel.Notes {
		if note.Detect == "" {
			continue
		}
		pair, ok := v086DetectPairs[note.Detect]
		if !ok {
			t.Errorf("%s note %d (%s): detect %q has no old/new pair in v086DetectPairs", version, i+1, note.Change, note.Detect)
			continue
		}
		seen[note.Detect] = true
		rel := fmt.Sprintf("note%02d/app.go", i+1)
		writeUpgradeFixture(t, oldDir, rel, "package app\n\n"+pair[0]+"\n")
		writeUpgradeFixture(t, newDir, rel, "package app\n\n"+pair[1]+"\n")
		if hits := detectHits(oldDir, note.Detect); len(hits) != 1 || !strings.HasSuffix(hits[0], rel+":3") {
			t.Errorf("%s note %d: detect %q must flag the old line %q, got hits %v", version, i+1, note.Detect, pair[0], hits)
		}
		if hits := detectHits(newDir, note.Detect); len(hits) != 0 {
			t.Errorf("%s note %d: detect %q must stay silent on the new spelling %q, got hits %v", version, i+1, note.Detect, pair[1], hits)
		}
	}
	for pattern := range v086DetectPairs {
		if !seen[pattern] {
			t.Errorf("v086DetectPairs has a pair for %q but no %s note carries that detect", pattern, version)
		}
	}
}

// TestV086TokenRenameDetectSeesEverySpelling runs the 2xl/3xl note's
// detect over each old spelling on its own: the CSS variable and the
// {category.name} reference ResolveAll turns into one. The old/new pair
// above carries both on one line, so it would still pass with either
// alternative of the regex deleted.
func TestV086TokenRenameDetectSeesEverySpelling(t *testing.T) {
	const detect = `--(spacing|text|breakpoint)-x{2,3}l\b|\{(spacing|typography|text|breakpoints?)\.x{2,3}l\}`
	if _, ok := v086DetectPairs[detect]; !ok {
		t.Fatalf("no v0.86.0 pair for %q; keep this test's detect equal to the note's", detect)
	}
	re := regexp.MustCompile(detect)
	for _, old := range []string{
		"--spacing-xxl", "--spacing-xxxl", "--text-xxl", "--text-xxxl", "--breakpoint-xxl",
		"{spacing.xxl}", "{spacing.xxxl}", "{typography.xxxl}", "{text.xxl}", "{breakpoints.xxl}", "{breakpoint.xxl}",
	} {
		if !re.MatchString(old) {
			t.Errorf("detect misses the old spelling %q", old)
		}
	}
	for _, cur := range []string{
		"--spacing-2xl", "--spacing-3xl", "--text-3xl", "--breakpoint-2xl",
		"{spacing.2xl}", "{typography.3xl}", "{breakpoints.2xl}", "--spacing-xl", "{spacing.xl}",
	} {
		if re.MatchString(cur) {
			t.Errorf("detect flags the current spelling %q", cur)
		}
	}
}
