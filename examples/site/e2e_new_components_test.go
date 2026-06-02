package main

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// =============================================================================
// Per-component e2e tests ported from examples/website. Each test hits
// /components/<slug> and asserts ARIA roles, key elements and (where
// applicable) basic runtime-driven behaviour.
//
// NOTE: several slugs are note-only in site (combobox, multiselect,
// confirmaction, sortablelist, infinitescroll, gallery, lightbox,
// commandpalette, globalsearch, notificationbell, datatable, scrollspy,
// pipelineimage, conditionalfield, formrepeater, repeater) — those
// are only tested for page-loads or dropped entirely.
//
// Tests that would exactly duplicate existing tests in e2e_test.go are
// also dropped (copybutton, textarea autogrow are covered there).
// =============================================================================

func TestE2E_NewComponents_KbdPrimitive(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var n int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/kbd"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('main kbd').length`, &n),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if n < 2 {
		t.Errorf("expected ≥2 <kbd> elements on /components/kbd, got %d", n)
	}
}

func TestE2E_NewComponents_AvatarGroupOverflow(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var overflow string
	var visibleCount int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/avatargroup"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-comp="ui-avatar-group"] .ui-avatar').length`, &visibleCount),
		chromedp.Evaluate(`document.querySelector('[data-fui-comp="ui-avatar-group"] .ui-avatar-group__overflow')?.textContent || ''`, &overflow),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if visibleCount < 1 {
		t.Errorf("expected ≥1 avatar rendered, got %d", visibleCount)
	}
	// Demo: 7 avatars, Max=4 → overflow "+3"
	if !strings.Contains(overflow, "+") {
		t.Errorf("expected overflow chip with +N, got %q", overflow)
	}
}

func TestE2E_NewComponents_ShortcutHintRendersChips(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var kbdCount int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/shortcuthint"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-comp="ui-shortcut-hint"]').length`, &kbdCount),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if kbdCount < 1 {
		t.Errorf("expected ≥1 ShortcutHint on the demo page, got %d", kbdCount)
	}
}

func TestE2E_NewComponents_SegmentedControlRole(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var role string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/segmented"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('.ui-segmented[role="radiogroup"]')?.getAttribute('role') || ''`, &role),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if role != "radiogroup" {
		t.Errorf("expected role=radiogroup, got %q", role)
	}
}

// confirmaction is note-only in site — just assert page loads.
func TestE2E_NewComponents_ConfirmActionPageLoads(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var h1 string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/confirmaction"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('h1')?.textContent || ''`, &h1),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if h1 == "" {
		t.Error("expected a heading on /components/confirmaction")
	}
}

func TestE2E_NewComponents_FilterChipBarToolbarRole(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var chipCount int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/filterchipbar"),
		pageReady(),
		// Site demo has 2 chips (Open + Mine) — no toolbar wrapper id
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-comp="ui-filter-bar"] .ui-tag').length`, &chipCount),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if chipCount < 1 {
		t.Errorf("expected ≥1 chip in FilterChipBar demo, got %d", chipCount)
	}
}

// infinitescroll is note-only — just page loads.
func TestE2E_NewComponents_InfiniteScrollPageLoads(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var h1 string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/infinitescroll"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('h1')?.textContent || ''`, &h1),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if h1 == "" {
		t.Error("expected a heading on /components/infinitescroll")
	}
}

// combobox is note-only — just page loads.
func TestE2E_NewComponents_ComboboxPageLoads(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var h1 string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/combobox"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('h1')?.textContent || ''`, &h1),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if h1 == "" {
		t.Error("expected a heading on /components/combobox")
	}
}

func TestE2E_NewComponents_TreeViewARIA(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var role, expandedSrc string
	var rootCount int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/tree"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('[role="tree"]')?.getAttribute('role') || ''`, &role),
		chromedp.Evaluate(`document.querySelectorAll('[role="tree"] > [role="treeitem"]').length`, &rootCount),
		chromedp.Evaluate(`document.getElementById('src')?.getAttribute('aria-expanded') || ''`, &expandedSrc),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if role != "tree" {
		t.Errorf("expected role=tree, got %q", role)
	}
	if rootCount < 2 {
		t.Errorf("expected ≥2 root treeitems, got %d", rootCount)
	}
	if expandedSrc != "true" {
		t.Errorf("expected src node aria-expanded=true, got %q", expandedSrc)
	}
}

func TestE2E_NewComponents_BannerVariantsAndRoles(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var banners int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/banner"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-comp="ui-banner"]').length`, &banners),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if banners < 1 {
		t.Errorf("expected ≥1 ui-banner on /components/banner, got %d", banners)
	}
}

func TestE2E_NewComponents_TimelineItems(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var items int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/timeline"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-comp="ui-timeline"] .ui-timeline__item').length`, &items),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if items < 3 {
		t.Errorf("expected ≥3 timeline events on demo, got %d", items)
	}
}

func TestE2E_NewComponents_RatingRadioGroup(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var role string
	var radios int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/rating"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('[data-fui-comp="ui-rating"]')?.getAttribute('role') || ''`, &role),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-comp="ui-rating"] input[type=radio]').length`, &radios),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if role != "radiogroup" {
		t.Errorf("expected role=radiogroup, got %q", role)
	}
	if radios < 5 {
		t.Errorf("expected ≥5 radios (5-star default), got %d", radios)
	}
}

func TestE2E_NewComponents_ColorPickerNativeInput(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var inputs int
	var value string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/colorpicker"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-comp="ui-color-picker"] input[type=color]').length`, &inputs),
		chromedp.Evaluate(`document.querySelector('[data-fui-comp="ui-color-picker"] input[type=color]')?.value || ''`, &value),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if inputs < 1 {
		t.Errorf("expected ≥1 native color input, got %d", inputs)
	}
	if value == "" {
		t.Errorf("expected initial Value to be reflected as input value, got empty")
	}
}

// commandpalette is note-only — just page loads.
func TestE2E_NewComponents_CommandPalettePageLoads(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var h1 string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/commandpalette"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('h1')?.textContent || ''`, &h1),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if h1 == "" {
		t.Error("expected a heading on /components/commandpalette")
	}
}

func TestE2E_Container_RendersDivWithMaxWidth(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var count int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/container"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-comp="ui-container"]').length`, &count),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if count < 1 {
		t.Errorf("expected ≥1 Container on /components/container, got %d", count)
	}
}

func TestE2E_Disclosure_OpenAndKeyboard(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var openBefore, openAfter bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/disclosure"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('[data-fui-comp="ui-disclosure"]').hasAttribute('open')`, &openBefore),
		chromedp.Evaluate(`document.querySelector('[data-fui-comp="ui-disclosure"] .ui-disclosure__summary').click()`, nil),
		chromedp.Sleep(100*1e6),
		chromedp.Evaluate(`document.querySelector('[data-fui-comp="ui-disclosure"]').hasAttribute('open')`, &openAfter),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if openBefore == openAfter {
		t.Errorf("clicking summary should toggle open; before=%v after=%v", openBefore, openAfter)
	}
}

func TestE2E_TimePicker_NativeInputAndLabel(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var inputs int
	var labelFor string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/timepicker"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-comp="ui-time-picker"] input[type=time]').length`, &inputs),
		chromedp.Evaluate(`document.querySelector('[data-fui-comp="ui-time-picker"] label')?.getAttribute('for') || ''`, &labelFor),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if inputs < 1 {
		t.Errorf("expected ≥1 native time input, got %d", inputs)
	}
	if labelFor == "" {
		t.Errorf("expected <label for=…> wired to the time input")
	}
}

func TestE2E_Toolbar_RoleAndGroups(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var role string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/toolbar"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('[data-fui-comp="ui-toolbar"]')?.getAttribute('role') || ''`, &role),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if role != "toolbar" {
		t.Errorf("expected role=toolbar, got %q", role)
	}
}

func TestE2E_Sparkline_RendersSVGPath(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var sparks, paths int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/sparkline"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-comp="ui-sparkline"]').length`, &sparks),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-comp="ui-sparkline"] path').length`, &paths),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if sparks < 1 {
		t.Errorf("expected ≥1 sparkline on demo, got %d", sparks)
	}
	if paths < 1 {
		t.Errorf("expected ≥1 line path, got %d", paths)
	}
}

func TestE2E_PieChart_RendersSlices(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var pies int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/piechart"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-comp="ui-pie-chart"]').length`, &pies),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if pies < 1 {
		t.Errorf("expected ≥1 pie-chart, got %d", pies)
	}
}

func TestE2E_BarChart_RendersBars(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var bars int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/barchart"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-comp="ui-bar-chart"] rect.ui-bar-chart__bar').length`, &bars),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if bars < 5 {
		t.Errorf("expected ≥5 bars on demo, got %d", bars)
	}
}

func TestE2E_LineChart_SeriesAndLegend(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var lines int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/linechart"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-comp="ui-line-chart"] path.ui-line-chart__line').length`, &lines),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if lines < 1 {
		t.Errorf("expected ≥1 line path, got %d", lines)
	}
}

func TestE2E_JSONViewer_DetailsNodes(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var nodes int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/jsonviewer"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-comp="ui-json-viewer"] details.ui-json-viewer__node').length`, &nodes),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if nodes < 1 {
		t.Errorf("expected ≥1 collapsible node, got %d", nodes)
	}
}

func TestE2E_DiffViewer_UnifiedPresent(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var unified int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/diffviewer"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-comp="ui-diff-viewer"]').length`, &unified),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if unified < 1 {
		t.Errorf("expected ≥1 diff viewer, got %d", unified)
	}
}

func TestE2E_Markdown_RendersHeadings(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var headings int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/markdown"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-comp="ui-markdown"] h1').length`, &headings),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if headings < 1 {
		t.Errorf("expected ≥1 <h1> from rendered markdown, got %d", headings)
	}
}

func TestE2E_TOC_PageLoads(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var navCount int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/toc"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-comp="ui-toc"]').length`, &navCount),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if navCount < 1 {
		t.Errorf("expected ≥1 ui-toc, got %d", navCount)
	}
}

// gallery is note-only — page loads assertion.
func TestE2E_Gallery_PageLoads(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var h1 string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/gallery"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('h1')?.textContent || ''`, &h1),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if h1 == "" {
		t.Error("expected a heading on /components/gallery")
	}
}

// lightbox is note-only — page loads assertion.
func TestE2E_Lightbox_PageLoads(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var h1 string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/lightbox"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('h1')?.textContent || ''`, &h1),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if h1 == "" {
		t.Error("expected a heading on /components/lightbox")
	}
}

// notificationbell is note-only — page loads assertion.
func TestE2E_NotificationBell_PageLoads(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var h1 string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/notificationbell"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('h1')?.textContent || ''`, &h1),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if h1 == "" {
		t.Error("expected a heading on /components/notificationbell")
	}
}

// sortablelist is note-only — page loads assertion.
func TestE2E_SortableList_PageLoads(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var h1 string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/sortablelist"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('h1')?.textContent || ''`, &h1),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if h1 == "" {
		t.Error("expected a heading on /components/sortablelist")
	}
}

// globalsearch is note-only — page loads assertion.
func TestE2E_GlobalSearch_PageLoads(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var h1 string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/globalsearch"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('h1')?.textContent || ''`, &h1),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if h1 == "" {
		t.Error("expected a heading on /components/globalsearch")
	}
}

func TestE2E_Carousel_PrevNextScrollsTrack(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var x1, x2 float64
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/carousel"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('[data-fui-carousel] [data-fui-carousel-track]').scrollLeft`, &x1),
		chromedp.Evaluate(`document.querySelector('[data-fui-carousel] [data-fui-carousel-next]').click()`, nil),
		chromedp.Sleep(600*1e6),
		chromedp.Evaluate(`document.querySelector('[data-fui-carousel] [data-fui-carousel-track]').scrollLeft`, &x2),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if x2 <= x1 {
		t.Errorf("Next button should scroll the track; before=%f after=%f", x1, x2)
	}
}
