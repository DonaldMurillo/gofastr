package uihost

import (
	"crypto/sha256"
	"encoding/hex"
	"html"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
)

var subsetBundleRe = regexp.MustCompile(`(?s)<style data-cui-bundle="([^"]*)" data-cui-bundle-href="([^"]*)">(.*?)</style>`)

func TestPruneCSS(t *testing.T) {
	has := func(t string) bool { return t == "fui-a" }
	for _, tc := range []struct{ in, want string }{
		{`.fui-a{x}.fui-b{y}`, `.fui-a{x}`},
		{`.fui-ab{y}`, ``},
		{`.fui-b,.other{y}`, `.fui-b,.other{y}`},
		{`.fui-b,.fui-c{y}`, ``},
		{`.fui-a .fui-b{y}`, ``},
		{`.fui-a:not(.fui-b){y}`, `.fui-a:not(.fui-b){y}`},
		{`:is(.fui-b,.fui-c){y}`, `:is(.fui-b,.fui-c){y}`},
		{`[class*="fui-b"]{y}`, `[class*="fui-b"]{y}`},
		{`.fui-b\:x{y}`, `.fui-b\:x{y}`},
		{`.a\.fui-b{y}`, `.a\.fui-b{y}`},
		{`.other{y}`, `.other{y}`},
		{`@media (min-width:1px){.fui-b{y}}`, ``},
		{`@media (min-width:1px){.fui-a{x}.fui-b{y}}`, `@media (min-width:1px){.fui-a{x}}`},
		{`@supports (display:grid){@media print{.fui-b{y}}.fui-a{x}}`, `@supports (display:grid){.fui-a{x}}`},
		{`@keyframes k{from{opacity:0}}`, `@keyframes k{from{opacity:0}}`},
		{`@layer a, b;.fui-b{y}`, `@layer a, b;`},
		{`/* .fui-b { */.fui-a{x}`, `.fui-a{x}`},
		{`.fui-a{content:"}"}.fui-b{y}`, `.fui-a{content:"}"}`},
		{`.fui-a{&.fui-b{y}}`, `.fui-a{&.fui-b{y}}`},
		{".fui-b\n{y}\n.fui-a\n{x}", ".fui-a\n{x}"},
		// An unquoted url() may hold braces; they are not blocks.
		{`.fui-a{background:url(/}.fui-b{);color:red}`, `.fui-a{background:url(/}.fui-b{);color:red}`},
		{`.fui-a{background:url(/*x);color:red}.fui-b{y}`, `.fui-a{background:url(/*x);color:red}`},
		// A layer block that empties still declares its layer: dropping
		// it would reorder the layers that follow.
		{`@layer a{.fui-b{y}}@layer b{.fui-a{x}}`, `@layer a{}@layer b{.fui-a{x}}`},
	} {
		got, ok := pruneCSS(tc.in, has)
		if !ok || got != tc.want {
			t.Errorf("pruneCSS(%q) = %q, %v; want %q", tc.in, got, ok, tc.want)
		}
	}
	for _, bad := range []string{`.fui-a{x`, `.fui-a{x}}`, `{x}`} {
		if _, ok := pruneCSS(bad, has); ok {
			t.Errorf("pruneCSS(%q) parsed an unbalanced sheet", bad)
		}
	}
}

// TestDroppableClassesReadsEverySelector: a subset is cached by which of
// these classes the page carries, so every class whose presence can
// change the subset must be in it, not only the first selector's. A list
// with a selector naming no fui- class always keeps its rule, so what
// follows that selector cannot change anything.
func TestDroppableClassesReadsEverySelector(t *testing.T) {
	got := droppableClasses(`.fui-a,.fui-b{x}.fui-c .fui-d,.fui-e{y}.other,.fui-f{z}`)
	if strings.Join(got, " ") != "fui-a fui-b fui-c fui-d fui-e" {
		t.Errorf("droppableClasses = %v, want fui-a through fui-e", got)
	}
}

func TestClassTokens(t *testing.T) {
	got := classTokens(`<p class="fui-a fui-a--x"><template><i class="fui-t"></i></template><b data-k="{fui-j}">`, nil)
	for _, want := range []string{"fui-a", "fui-a--x", "fui-t", "fui-j"} {
		if _, ok := got[want]; !ok {
			t.Errorf("classTokens missed %s in %v", want, got)
		}
	}
	if len(got) != 4 {
		t.Errorf("classTokens = %v, want four tokens", got)
	}
}

// heavySubsetStyle registers a sheet too large for the first-paint budget
// whose rules are almost all on fui- classes no page renders, plus the
// rules for the classes in used.
func heavySubsetStyle(t *testing.T, used ...string) *registry.Style {
	t.Helper()
	name := uniqueName("fp-sub")
	return registry.RegisterStyle(name, func(theme style.Theme) string {
		sh := style.NewComponentSheet(name, theme)
		for _, c := range used {
			sh = sh.Rule("."+c).Set("color", "rgb(1, 2, 3)").End()
		}
		for i := range 2500 {
			sum := sha256.Sum256([]byte(strconv.Itoa(i)))
			h := hex.EncodeToString(sum[:8])
			sh = sh.Rule(".fui-unused-"+h).Set("color", "#"+h[:6]).End()
		}
		return sh.MustBuild()
	})
}

type classPage struct {
	style   *registry.Style
	classes []string
	also    []*registry.Style // rendered after, each around a plain section
}

func (p *classPage) Render() render.HTML {
	var b strings.Builder
	for _, c := range p.classes {
		b.WriteString(`<p class="` + c + `">x</p>`)
	}
	out := p.style.Render(&rawHTML{`<section>` + b.String() + `</section>`})
	for _, s := range p.also {
		out += s.Render(&innerSpan{body: "also"})
	}
	return out
}

type rawHTML struct{ s string }

func (r *rawHTML) Render() render.HTML { return render.HTML(r.s) }

func newClassPageHost(s *registry.Style, classes ...string) *UIHost {
	application := app.NewApp("Test App")
	application.SetDefaultLayout(chromeTestLayout("main", &testHeaderComp{}, nil, &testFooterComp{}))
	application.RegisterScreen(app.NewScreen("/", &classPage{style: s, classes: classes}).WithTitle("Home"), nil)
	return New(application)
}

// TestFirstPaintSubsetInlinesWhatThePageUses: a page whose bundle is too
// heavy for the first round trip, but whose markup uses a sliver of it,
// inlines the sliver, names the full bundle beside it, and allows the
// sliver by hash. The URL serves the full bundle, the one the linked
// head would have named.
func TestFirstPaintSubsetInlinesWhatThePageUses(t *testing.T) {
	registry.IsolateForTest(t)
	s := heavySubsetStyle(t, "fui-sub-used", "fui-sub-other")
	ds := newClassPageHost(s, "fui-sub-used")
	w := servePage(t, ds, "/")
	body := w.Body.String()
	m := subsetBundleRe.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no subset bundle in:\n%s", truncate(body, 1500))
	}
	if m[1] != s.Name() {
		t.Errorf("data-cui-bundle = %q, want %q", m[1], s.Name())
	}
	sub := m[3]
	if !strings.Contains(sub, ".fui-sub-used") {
		t.Error("the subset dropped the rule the page renders")
	}
	if strings.Contains(sub, "fui-unused-") || strings.Contains(sub, ".fui-sub-other") {
		t.Error("the subset kept rules for classes the page does not render")
	}
	if !strings.Contains(w.Header().Get("Content-Security-Policy"), cspHash(sub)) {
		t.Error("the policy does not allow the subset by hash")
	}
	if !inlineAppRe.MatchString(body) {
		t.Error("app.css was not inlined beside the subset")
	}

	href := html.UnescapeString(m[2])
	linked := linkedPageBody(t, newClassPageHost(s, "fui-sub-used"), "/")
	if !strings.Contains(linked, `href="`+href+`"`) {
		t.Errorf("data-cui-bundle-href %q is not the URL the linked head names", href)
	}
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, href, nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), ".fui-sub-other") ||
		!strings.Contains(rec.Body.String(), "fui-unused-") {
		t.Errorf("the full bundle URL served %d without the dropped rules", rec.Code)
	}
}

// TestFirstPaintSubsetOverBudgetLinks: when the page uses enough of the
// bundle that even its subset misses the budget, the head links the full
// bundle as before.
func TestFirstPaintSubsetOverBudgetLinks(t *testing.T) {
	registry.IsolateForTest(t)
	var used []string
	for i := range 2000 {
		sum := sha256.Sum256([]byte(strconv.Itoa(i)))
		used = append(used, "fui-unused-"+hex.EncodeToString(sum[:8]))
	}
	s := heavySubsetStyle(t)
	w := servePage(t, newClassPageHost(s, used...), "/")
	body := w.Body.String()
	if subsetBundleRe.MatchString(body) || inlineAppRe.MatchString(body) {
		t.Fatal("a subset over the budget was inlined")
	}
	if !strings.Contains(body, `rel="stylesheet" href="/__gofastr/comp/`+s.Name()+`.css?v=`) {
		t.Error("the page lost its stylesheet link")
	}
}

// TestFirstPaintFittingBundleShipsWhole: a bundle that fits inlines whole,
// with no full-bundle URL, whatever its markup uses.
func TestFirstPaintFittingBundleShipsWhole(t *testing.T) {
	registry.IsolateForTest(t)
	name := uniqueName("fp-whole")
	s := registry.RegisterStyle(name, func(theme style.Theme) string {
		return style.NewComponentSheet(name, theme).
			Rule(".fui-whole-a").Set("color", "red").End().
			Rule(".fui-whole-b").Set("color", "blue").End().
			MustBuild()
	})
	body := servePage(t, newClassPageHost(s, "fui-whole-a"), "/").Body.String()
	m := inlineBundleRe.FindStringSubmatch(body)
	if m == nil || subsetBundleRe.MatchString(body) {
		t.Fatalf("the fitting bundle did not inline whole:\n%s", truncate(body, 1500))
	}
	if !strings.Contains(m[2], ".fui-whole-b") {
		t.Error("a fitting bundle was pruned")
	}
}

// TestFirstPaintSubsetKeepsLateClasses: the subset keeps rules for the
// classes the planner is handed beside the page (the widgets the
// response adds after the chrome) and the classes a registered behaviour
// adds at run time.
func TestFirstPaintSubsetKeepsLateClasses(t *testing.T) {
	registry.IsolateForTest(t)
	registry.RegisterBehavior("fp-sub-beh", `el.classList.add('fui-sub-beh')`, registry.Markers("[data-fp-sub-beh]"))
	s := heavySubsetStyle(t, "fui-sub-used", "fui-sub-widget", "fui-sub-beh")
	ds := newClassPageHost(s, "fui-sub-used")
	page := `<p class="fui-sub-used" data-cui-comp="` + s.Name() + `"></p>`
	p, ok := ds.planFirstPaint([]string{s.Name()}, page, `<div class="fui-sub-widget"></div>`)
	if !ok || p.comp == nil || p.comp.href == "" {
		t.Fatalf("no subset planned: %+v %v", p, ok)
	}
	for _, c := range []string{".fui-sub-used", ".fui-sub-widget", ".fui-sub-beh"} {
		if !strings.Contains(p.comp.body, c) {
			t.Errorf("the subset dropped %s", c)
		}
	}
}

// TestFirstPaintSubsetSeesWidgetChrome: a widget the response inlines
// after the chrome (here a deep-linked dialog) renders classes the page
// itself does not; the subset the head carries keeps their rules.
func TestFirstPaintSubsetSeesWidgetChrome(t *testing.T) {
	registry.IsolateForTest(t)
	// The widget registry is process-global with no reset: a deep-link
	// widget inlines only on its own URL, so no other test sees it.
	widget.MountBuilder(router.New(), widget.New("fp-sub-dialog").
		Mount(widget.Center).
		Hidden().
		DeepLink("modal", "fp-sub").
		Slot("body", &rawHTML{`<p class="fui-sub-widget">w</p>`}))
	s := heavySubsetStyle(t, "fui-sub-used", "fui-sub-widget")
	body := servePage(t, newClassPageHost(s, "fui-sub-used"), "/?modal=fp-sub").Body.String()
	if !strings.Contains(body, `class="fui-sub-widget"`) {
		t.Fatal("the deep-linked widget was not inlined")
	}
	m := subsetBundleRe.FindStringSubmatch(body)
	if m == nil {
		t.Fatal("no subset bundle")
	}
	if !strings.Contains(m[3], ".fui-sub-widget") {
		t.Error("the subset dropped the rule the inlined widget renders")
	}
}
