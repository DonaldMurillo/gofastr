package main

// The section layers (layout.go): the taught hubs, the examples section and
// the plugins registry each own a ScreenGroup layer under the default
// layout. A client navigation between two pages of one section must keep
// that layer's DOM (its nav column, its rail) and swap only the primary
// slot plus the route's outlet fills; leaving the section replaces it.
//
// Each test tags the kept node with an expando before the click. A swap
// that rebuilt the layer would drop the tag even when the new markup looks
// identical, so the tag surviving is the "kept, not re-rendered" proof.

import (
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// sectionLayerNav navigates to from, tags the node matching keep, clicks
// the link to href (by CSS selector in the primary slot or the nav), and
// reports the destination path, whether the tagged node survived, and the
// textContent of probe afterwards.
func sectionLayerNav(t *testing.T, from, keep, click, probe string) (path string, kept bool, probed string) {
	t.Helper()
	base := siteE2EServer(t)
	ctx := siteBrowserCtx(t)
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+from),
		chromedp.WaitReady(keep, chromedp.ByQuery),
		waitModule("window.__gofastr && window.__gofastr.loadedModules"),
		chromedp.Evaluate(`document.querySelector(`+jsString(keep)+`).__keepTag = 1`, nil),
		chromedp.Evaluate(`document.querySelector(`+jsString(click)+`).click()`, nil),
		chromedp.Sleep(900*time.Millisecond),
		chromedp.Evaluate(`location.pathname`, &path),
		chromedp.Evaluate(`(document.querySelector(`+jsString(keep)+`) || {}).__keepTag === 1`, &kept),
		chromedp.Evaluate(`(document.querySelector(`+jsString(probe)+`) || {textContent: ""}).textContent`, &probed),
	); err != nil {
		t.Fatalf("section layer nav %s → %s: %v", from, click, err)
	}
	return path, kept, probed
}

// jsString quotes s as a JS single-quoted string literal (selectors here
// are test constants; this only escapes the quote and backslash).
func jsString(s string) string {
	out := []byte{'\''}
	for i := 0; i < len(s); i++ {
		if s[i] == '\'' || s[i] == '\\' {
			out = append(out, '\\')
		}
		out = append(out, s[i])
	}
	return string(append(out, '\''))
}

// Hub → hub: the hub layer (reading column + rail) is kept, the primary is
// the new hub, and the toc outlet carries the new hub's concepts.
func TestE2E_SectionLayers_HubKeepsFrameAndRefillsTOC(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	const layer = `[data-cui-layout-key="g:/:hub"]`
	path, kept, toc := sectionLayerNav(t, "/primitives", layer,
		`[data-cui-layout-key="g:/:hub"] a[href="/framework"]`, // the "Where next" card
		`[data-cui-layout-key="g:/:hub"] [data-cui-comp="ui-toc"]`)
	if path != "/framework" {
		t.Fatalf("path after hub link = %q, want /framework", path)
	}
	if !kept {
		t.Error("the hub layer was rebuilt on hub→hub navigation; only its primary and the toc fill should swap")
	}
	for _, want := range []string{"Entities", "Migrations", "Theming"} {
		if !strings.Contains(toc, want) {
			t.Errorf("toc after nav lacks the /framework concept %q (got %q): the outlet fill did not follow the route", want, toc)
		}
	}
}

// Plugins index → one plugin: the registry nav column is the same node.
func TestE2E_SectionLayers_PluginNavColumnKept(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	const nav = `[data-cui-layout-key="g:/plugins/:plugins"] .fui-content-row__nav`
	path, kept, h1 := sectionLayerNav(t, "/plugins", nav,
		`[data-cui-layout-key="g:/plugins/:plugins"] .layout-content a[href="/plugins/mermaid"]`,
		`h1`)
	if path != "/plugins/mermaid" {
		t.Fatalf("path = %q, want /plugins/mermaid", path)
	}
	if !kept {
		t.Error("the plugins nav column was rebuilt on a sibling navigation")
	}
	if h1 != "mermaid" {
		t.Errorf("primary h1 = %q, want the plugin page's", h1)
	}
}

// Examples: the examples nav column survives a move between two demos.
func TestE2E_SectionLayers_ExamplesNavColumnKept(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	const nav = `[data-cui-layout-key="g:/examples/:examples"] .fui-content-row__nav`
	path, kept, h1 := sectionLayerNav(t, "/examples/workspace", nav,
		nav+` a[href="/examples/catalog"]`, `h1`)
	if path != "/examples/catalog" {
		t.Fatalf("path = %q, want /examples/catalog", path)
	}
	if !kept {
		t.Error("the examples nav column was rebuilt on a sibling navigation")
	}
	if h1 != "Catalog" {
		t.Errorf("primary h1 = %q, want Catalog", h1)
	}
}

// Leaving a section is a different branch: the section layer goes away.
func TestE2E_SectionLayers_LeavingSectionDropsLayer(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	const nav = `[data-cui-layout-key="g:/plugins/:plugins"]`
	path, kept, _ := sectionLayerNav(t, "/plugins", nav,
		`[data-cui-scope="docsite-header"] a[href="/primitives"]`, `h1`)
	if path != "/primitives" {
		t.Fatalf("path = %q, want /primitives", path)
	}
	if kept {
		t.Error("the plugins layer survived navigating to a hub; the chains share only the main layer")
	}
}

// Docs: a doc-to-doc navigation keeps the docs layer. The docs nav (the
// whole catalog in one SectionMenu) is the same node afterwards, keeps
// the scroll the reader gave it, and moves its current-page mark; the
// article, the crumbs and the pager follow the route as the primary and
// the layer's outlet fills.
func TestE2E_SectionLayers_DocsNavKeptAcrossDocs(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	flat := flatDocs()
	dest := flat[len(flat)-3] // far down the rail, so reaching it means scrolling it
	const layer = `[data-cui-layout-key="g:/docs/:docs"]`
	// The rail is found by its own name, not through the layer, so a
	// regression that draws the nav per screen fails the kept/scroll
	// assertions below instead of timing out on a missing layer.
	const rail = `nav[aria-label="Documentation sections"] .cui-section-menu__rail`
	destLink := rail + ` a[href="/docs/` + dest.Slug + `"]`

	base := siteE2EServer(t)
	ctx := siteBrowserCtx(t)
	var path, crumbs, current, prevHref string
	var kept bool
	var scrollBefore, scrollAfter float64
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/docs/query-dsl"),
		chromedp.WaitReady(rail, chromedp.ByQuery),
		waitModule("window.__gofastr && window.__gofastr.loadedModules"),
		chromedp.Evaluate(`(()=>{const r=document.querySelector(`+jsString(rail)+`); r.__keepTag=1; r.scrollTop=r.scrollHeight; return r.scrollTop})()`, &scrollBefore),
		chromedp.Evaluate(`document.querySelector(`+jsString(destLink)+`).click()`, nil),
		chromedp.Sleep(900*time.Millisecond),
		chromedp.Evaluate(`location.pathname`, &path),
		chromedp.Evaluate(`(document.querySelector(`+jsString(rail)+`) || {}).__keepTag === 1`, &kept),
		chromedp.Evaluate(`(document.querySelector(`+jsString(rail)+`) || {scrollTop: -1}).scrollTop`, &scrollAfter),
		chromedp.Evaluate(`(document.querySelector(`+jsString(rail+` a[aria-current="page"]`)+`) || {}).getAttribute ? document.querySelector(`+jsString(rail+` a[aria-current="page"]`)+`).getAttribute("href") : ""`, &current),
		chromedp.Evaluate(`(document.querySelector(`+jsString(layer+` [data-cui-outlet$="#crumbs"]`)+`) || {textContent: ""}).textContent`, &crumbs),
		chromedp.Evaluate(`(()=>{const a=document.querySelector(`+jsString(layer+` [data-cui-outlet$="#pager"] a`)+`);return a?a.getAttribute("href"):""})()`, &prevHref),
	); err != nil {
		t.Fatalf("docs layer nav: %v", err)
	}
	if path != "/docs/"+dest.Slug {
		t.Fatalf("path = %q, want /docs/%s", path, dest.Slug)
	}
	if !kept {
		t.Error("the docs nav was rebuilt on a doc-to-doc navigation; only the article and the outlet fills should swap")
	}
	if scrollBefore <= 0 {
		t.Fatalf("the docs rail did not scroll (scrollTop %v): the test cannot prove scroll is kept", scrollBefore)
	}
	if scrollAfter != scrollBefore {
		t.Errorf("docs rail scrollTop = %v after the navigation, want the reader's %v", scrollAfter, scrollBefore)
	}
	if current != "/docs/"+dest.Slug {
		t.Errorf("rail current link = %q, want /docs/%s (the active-link sweep did not move the mark)", current, dest.Slug)
	}
	if !strings.Contains(crumbs, dest.Title) {
		t.Errorf("crumbs after nav = %q, want the destination's title %q: the crumbs fill did not follow the route", crumbs, dest.Title)
	}
	if want := "/docs/" + flat[len(flat)-4].Slug; prevHref != want {
		t.Errorf("pager's first card = %q, want the previous doc %q: the pager fill did not follow the route", prevHref, want)
	}
}
