package desktopui_test

import (
	"strings"
	"testing"

	desktopui "github.com/DonaldMurillo/gofastr/battery/desktop/ui"
	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// The demo page: one screen through the real host pipeline (framework
// App, uihost mount, desktop theme, desktop layout), rendering every
// desktop component. This is the bundle proof: when a page uses the
// components, their registered styles are served.
type demoScreen struct{}

func (s *demoScreen) ScreenTitle() string       { return "Desktop UI demo" }
func (s *demoScreen) ScreenDescription() string { return "Every desktop component on one page" }

func (s *demoScreen) Render() render.HTML {
	return ui.Stack(ui.StackConfig{}, []render.HTML{
		desktopui.FloatingToolbar(ui.ToolbarConfig{
			Label:  "Demo actions",
			Groups: []ui.ToolbarGroup{{Children: []render.HTML{plain(`<button>Start</button>`)}}},
		}),
		desktopui.Inspector(desktopui.InspectorConfig{
			Label: "Demo inspector",
			Title: "Selection",
			Items: []ui.DetailItem{{Label: "Phase", Value: render.Text("Work")}},
		}),
		desktopui.Sheet(desktopui.SheetConfig{Title: "Discard draft?"},
			plain(`<p>The draft is unsaved.</p>`)),
		desktopui.Popover(desktopui.PopoverConfig{Title: "Quick actions"},
			plain(`<p>Pick one.</p>`)),
	}...)
}

// demoApp assembles the framework App the way a host does: the theme on
// the UI app, screens registered with the desktop layout, uihost
// mounted, everything behind the in-memory harness.
func demoApp(t *testing.T) *framework.TestApp {
	t.Helper()
	site := appui.NewApp("desktop-ui-demo")
	site.WithTheme(desktopui.Theme())
	sidebar := appui.NewStaticComponent(desktopui.SourceList(desktopui.SourceListConfig{
		Sections: []desktopui.SourceSection{{
			Title: "Views",
			Items: []desktopui.SourceItem{{Label: "Demo", Href: "/"}},
		}},
	}))
	site.Register("/", &demoScreen{}, desktopui.Layout().WithSidebar(sidebar))
	host := uihost.New(site)
	return framework.TestHarness(t, framework.NewUIHostApp(host,
		framework.WithConfig(framework.AppConfig{Name: "desktop-ui-demo"})))
}

// A page that uses the desktop components gets their stylesheets: the
// page links every used component (plus the LoadAlways layout sheet),
// and the linked CSS serves the real rules, not an empty shell.
func TestDemoPageBundlesDesktopStyles(t *testing.T) {
	ta := demoApp(t)
	body := ta.Get("/").Body()

	// The page rendered through the desktop layout with the sidebar.
	for _, w := range []string{
		`data-fui-layout="desktop"`,
		`data-fui-comp="desktopui-sourcelist"`,
		`data-fui-comp="desktopui-glass"`,
		`data-fui-comp="desktopui-floating-toolbar"`,
		`data-fui-comp="desktopui-inspector"`,
		`data-fui-comp="desktopui-sheet"`,
		`data-fui-comp="desktopui-popover"`,
	} {
		if !strings.Contains(body, w) {
			t.Errorf("demo page missing %q", w)
		}
	}

	// Every used component's stylesheet is linked: multi-component
	// pages take the single comp-bundle.css?names=… link (the desktop
	// components all appear in its names list), single-component pages
	// take per-component links. Either form must name every component
	// the page used, plus the eager layout sheet no marker names.
	for _, name := range []string{
		"desktopui-layout",
		"desktopui-sourcelist",
		"desktopui-glass",
		"desktopui-floating-toolbar",
		"desktopui-inspector",
		"desktopui-sheet",
		"desktopui-popover",
	} {
		if !strings.Contains(body, name+".css") && !bundleLists(body, name) {
			t.Errorf("demo page does not link %s.css:\n%s", name, excerptLinks(body))
		}
	}

	// The linked component CSS serves the real rules under the desktop
	// theme: the layout's zone knobs, the glass recipe, the source
	// list's capsule.
	layoutCSS := ta.Get("/__gofastr/comp/desktopui-layout.css").Body()
	for _, w := range []string{
		"--desktop-traffic-inset: 12px;",
		"background-color: transparent;",
	} {
		if !strings.Contains(layoutCSS, w) {
			t.Errorf("served desktopui-layout.css missing %q:\n%s", w, layoutCSS)
		}
	}
	glassCSS := ta.Get("/__gofastr/comp/desktopui-glass.css").Body()
	if !strings.Contains(glassCSS, "backdrop-filter:") {
		t.Errorf("served desktopui-glass.css missing the blur recipe:\n%s", glassCSS)
	}
	listCSS := ta.Get("/__gofastr/comp/desktopui-sourcelist.css").Body()
	if !strings.Contains(listCSS, "var(--radii-full") {
		t.Errorf("served desktopui-sourcelist.css missing the capsule:\n%s", listCSS)
	}

	// app.css carries the desktop theme's :root tokens, the applied
	// theme proving itself through the host pipeline.
	appCSS := ta.Get("/__gofastr/app.css").Body()
	for _, w := range []string{
		"--text-base: 13px;",
		"--font-body: -apple-system, system-ui, ui-sans-serif, sans-serif;",
		"--color-accent: #007AFF;",
	} {
		if !strings.Contains(appCSS, w) {
			t.Errorf("app.css missing the desktop token %q", w)
		}
	}
}

// bundleLists reports whether the page's comp-bundle link names the
// component.
func bundleLists(body, name string) bool {
	i := strings.Index(body, "data-fui-bundle=\"")
	if i == -1 {
		return false
	}
	rest := body[i+len(`data-fui-bundle="`):]
	j := strings.Index(rest, "\"")
	if j == -1 {
		return false
	}
	for _, n := range strings.Split(rest[:j], ",") {
		if n == name {
			return true
		}
	}
	return false
}

// excerptLinks lists the stylesheet links for a failure message.
func excerptLinks(body string) string {
	var out []string
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, ".css") {
			out = append(out, strings.TrimSpace(line))
		}
	}
	return strings.Join(out, "\n")
}
