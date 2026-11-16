package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// sweepLayoutRule returns the declarations from selector up to the next closing brace.
func sweepLayoutRule(t *testing.T, css, selector string) string {
	t.Helper()
	i := strings.Index(css, selector)
	if i < 0 {
		t.Fatalf("selector %q not found in:\n%s", selector, css)
	}
	rest := css[i:]
	j := strings.Index(rest, "}")
	if j < 0 {
		t.Fatalf("selector %q has no closing brace", selector)
	}
	return rest[:j]
}

func TestSweepLayoutPaneHostBreakpointMatchesModule(t *testing.T) {
	js, err := os.ReadFile(filepath.Join("..", "headless", "panehost.js"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`const MQ = '([^']+)'`).FindSubmatch(js)
	if m == nil {
		t.Fatal("framework/headless/panehost.js declares no const MQ")
	}
	if css := paneHostCSS(style.Theme{}); !strings.Contains(css, "@media "+string(m[1])+" {") {
		t.Errorf("the ui-pane-host sheet collapses at a different width than the module's MQ %s", m[1])
	}
}

func TestSweepLayoutContentRowPosturesAreExclusive(t *testing.T) {
	scopes := map[string]string{
		"48rem": ":not(.fui-content-row--stack-below-lg)", "47.99rem": ":not(.fui-content-row--stack-below-lg)",
		"64rem": ".fui-content-row--stack-below-lg", "63.99rem": ".fui-content-row--stack-below-lg",
	}
	width := regexp.MustCompile(`^@media \((?:min|max)-width: ([0-9.]+rem)\)`)
	scope, checked := "", 0
	for _, line := range strings.Split(contentRowCSS(style.Theme{}), "\n") {
		if m := width.FindStringSubmatch(line); m != nil {
			scope = scopes[m[1]]
			continue
		}
		if line == "}" {
			scope = ""
			continue
		}
		if scope == "" || !strings.Contains(line, ".fui-content-row") || strings.Contains(line, "/*") {
			continue
		}
		checked++
		if !strings.Contains(line, scope) {
			t.Errorf("a breakpoint rule is not scoped to its posture %q: %s", scope, line)
		}
	}
	if checked == 0 {
		t.Fatal("no breakpoint rules found")
	}
}

func TestSweepLayoutContentRowClaimsNoViewportHeight(t *testing.T) {
	vh := regexp.MustCompile(`\d+d?vh\b`)
	for _, line := range strings.Split(contentRowCSS(style.Theme{}), "\n") {
		if vh.MatchString(line) && !strings.Contains(line, "fui-content-row--viewport") && !strings.Contains(line, "fui-content-row--sticky") {
			t.Errorf("a content row rule outside Viewport and Sticky sizes to the viewport: %s", line)
		}
	}
}

func TestSweepLayoutSelfScrollingRegionsResetStickyTop(t *testing.T) {
	md := rowBreakpointCSS{scope: ":not(.fui-content-row--stack-below-lg)", maxw: "47.99rem", minw: "48rem"}
	cases := []struct{ name, css, selector string }{
		{"ListDetail list", listDetailCSS(style.Theme{}), ".fui-list-detail__list {"},
		{"Workbench rail", workbenchCSS(style.Theme{}), `[data-cui-comp="ui-workbench"] .fui-workbench__rail {`},
		{"PaneHost drawer", paneHostCSS(style.Theme{}), `[data-hui-pane-mode="overlay"] [data-hui-pane="tertiary"]:not([hidden]) {`},
		{"Box bounded", layoutCSS(style.Theme{}), ".fui-box--bounded {"},
		{"ContentRow sticky nav", md.stickyCSS(), ".fui-content-row__nav {"},
		{"ContentRow viewport columns", md.viewportCSS(), ".fui-content-row__workspace > .layout-content { min-block-size: 0; overflow-y: auto;"},
	}
	for _, c := range cases {
		if !strings.Contains(sweepLayoutRule(t, c.css, c.selector), "--ui-sticky-top: 0px") {
			t.Errorf("%s scrolls on its own but does not reset --ui-sticky-top to 0px", c.name)
		}
	}
}

func TestSweepLayoutTopStickyPartsReadStickyTop(t *testing.T) {
	cases := []struct{ name, css, selector string }{
		{"AnchoredRail", anchoredRailCSS(style.Theme{}), `[data-cui-comp="ui-anchored-rail"] {`},
		{"Sticky top", stickyCSS(style.Theme{}), ".fui-sticky--top {"},
		{"Sticky offset", stickyCSS(style.Theme{}), ".fui-sticky--offset-md {"},
		{"ContentRow sticky frame", contentRowCSS(style.Theme{}), ".fui-content-row--sticky {"},
	}
	for _, c := range cases {
		if !strings.Contains(sweepLayoutRule(t, c.css, c.selector), "var(--ui-sticky-top, 0px)") {
			t.Errorf("%s sticks without reading --ui-sticky-top", c.name)
		}
	}
}

func TestSweepLayoutAnchoredRailStylesBothActiveSpellings(t *testing.T) {
	rules := 0
	for _, r := range regexp.MustCompile(`([^{}]+)\{[^}]*\}`).FindAllStringSubmatch(anchoredRailCSS(style.Theme{}), -1) {
		sel := r[1]
		if !strings.Contains(sel, "is-active") && !strings.Contains(sel, `aria-current="true"`) {
			continue
		}
		rules++
		if !strings.Contains(sel, "a.is-active") || !strings.Contains(sel, `a[aria-current="true"]`) {
			t.Errorf("an active-entry rule styles one spelling only: %s", sel)
		}
	}
	if rules < 2 {
		t.Errorf("want the link and eyebrow active rules, found %d", rules)
	}
}

func TestSweepLayoutListDetailCarriesOnlyVTWiring(t *testing.T) {
	out := string(ListDetail(ListDetailConfig{ListLabel: "Issues", ExtraAttrs: map[string]string{
		"data-cui-vt": "detail", "data-cui-vt-when": "nav", "data-cui-rpc": "forged",
		"DATA-CUI-POLL": "5s", "data-fui-pane": "forged", "data-test": "hook",
	}}))
	for _, want := range []string{`data-cui-vt="detail"`, `data-cui-vt-when="nav"`, `data-test="hook"`} {
		if !strings.Contains(out, want) {
			t.Errorf("ListDetail dropped %s:\n%s", want, out)
		}
	}
	for _, bad := range []string{"data-cui-rpc", "forged", "5s"} {
		if strings.Contains(strings.ToLower(out), strings.ToLower(bad)) {
			t.Errorf("ListDetail forwarded runtime wiring %s:\n%s", bad, out)
		}
	}
}

func TestSweepLayoutListDetailRequiresListLabel(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("ListDetail rendered a list with no label")
		}
	}()
	ListDetail(ListDetailConfig{List: render.Text("rows")})
}

func TestSweepLayoutResponsiveRulesUseChildCombinator(t *testing.T) {
	css := responsiveCSS(style.Theme{})
	rules := regexp.MustCompile(`\.fui-responsive[^{]*\{ display: none`).FindAllString(css, -1)
	if len(rules) != 4 {
		t.Fatalf("want four hiding rules, got %d:\n%s", len(rules), css)
	}
	for _, r := range rules {
		if !regexp.MustCompile(`\) > \.fui-responsive__(desktop|mobile) \{|-lg > \.fui-responsive__(desktop|mobile) \{`).MatchString(r) {
			t.Errorf("a Responsive rule reaches past its own variants: %s", r)
		}
	}
}

func TestSweepLayoutSheetCompoundsMarkerAndClass(t *testing.T) {
	css := layoutCSS(style.Theme{})
	if regexp.MustCompile(`\[data-cui-comp="ui-layout"\]\)? \.fui-`).MatchString(css) {
		t.Errorf("the ui-layout sheet reaches a layout class as a descendant of the marker, not on the same element")
	}
	if !strings.Contains(css, `[data-cui-comp="ui-layout"].fui-grid`) {
		t.Errorf("the ui-layout sheet does not compound the marker with .fui-grid")
	}
}

func TestSweepLayoutGridMinLadderCoversWholeRems(t *testing.T) {
	css := layoutCSS(style.Theme{})
	for n := 1; n <= 64; n++ {
		want := fmt.Sprintf(`.fui-grid[data-min="%drem"] { --ui-grid-min: %drem; }`, n, n)
		if !strings.Contains(css, want) {
			t.Errorf("the data-min fallback ladder misses %drem", n)
		}
	}
	if !strings.Contains(css, "attr(data-min type(<length>), 16rem)") {
		t.Error("the ui-layout sheet does not read data-min through typed attr() with the 16rem default")
	}
}

func TestSweepLayoutHeroNamedByTitle(t *testing.T) {
	out := string(Hero(HeroConfig{Title: "Ship faster"}))
	if !strings.Contains(out, `aria-label="Ship faster"`) {
		t.Errorf("a Hero with no AriaLabel is not named by its Title:\n%s", out)
	}
	out = string(Hero(HeroConfig{Title: "Ship faster", AriaLabel: "Intro"}))
	if !strings.Contains(out, `aria-label="Intro"`) || strings.Contains(out, `aria-label="Ship faster"`) {
		t.Errorf("AriaLabel does not replace the Title as the Hero's name:\n%s", out)
	}
}

func TestSweepLayoutHeroSplitAriaLabelWinsOverHeadingID(t *testing.T) {
	out := string(HeroSplit(HeroSplitConfig{AriaLabel: "Intro", HeadingID: "intro-h"}))
	if !strings.Contains(out, `aria-label="Intro"`) || strings.Contains(out, "aria-labelledby") {
		t.Errorf("HeroSplit with AriaLabel and HeadingID must be named by AriaLabel alone:\n%s", out)
	}
}

func TestSweepLayoutPaneHostOpensOnlyAFilledPane(t *testing.T) {
	out := string(PaneHost(PaneHostConfig{Primary: render.Text("p"), SecondaryOpen: true, TertiaryOpen: true}))
	if strings.Contains(out, "-open") {
		t.Errorf("a side pane with no content renders open:\n%s", out)
	}
}

func TestSweepLayoutPaneHostColumnsFollowTheHook(t *testing.T) {
	css := paneHostCSS(style.Theme{})
	media := strings.Index(css, "@media")
	rules := 0
	for _, r := range regexp.MustCompile(`([^{}]+)\{([^}]*)\}`).FindAllStringSubmatch(css[:media], -1) {
		if !strings.Contains(r[2], "-w, ") || !strings.Contains(r[2], "grid-template-columns") {
			continue
		}
		rules++
		if !strings.Contains(r[1], "-open") || !strings.Contains(r[1], "[data-hui-pane-open~=") {
			t.Errorf("a pane-host column rule does not key off both the open class and data-hui-pane-open: %s", r[1])
		}
	}
	if rules != 3 {
		t.Errorf("want three column rules, found %d", rules)
	}
}

func TestSweepLayoutContentRowEmptyAsideReleasesWidth(t *testing.T) {
	if !strings.Contains(contentRowCSS(style.Theme{}), ".fui-content-row__aside:has(> [data-cui-outlet]:empty) { display: none; }") {
		t.Error("an aside holding only an empty outlet keeps its column")
	}
}

func TestSweepLayoutContentRowStickyNavDoesNotStretch(t *testing.T) {
	md := rowBreakpointCSS{scope: ":not(.fui-content-row--stack-below-lg)", maxw: "47.99rem", minw: "48rem"}
	if !strings.Contains(sweepLayoutRule(t, md.stickyCSS(), ".fui-content-row__nav {"), "align-self: flex-start") {
		t.Error("the sticky nav column stretches to the row's height and has nothing to stick within")
	}
}
