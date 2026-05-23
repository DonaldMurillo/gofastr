package main

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// =============================================================================
// Per-component e2e tests for the 11 new UI components. Each test hits the
// per-component demo page at /components/<slug> and asserts ARIA roles,
// keyboard nav, and (where applicable) runtime-driven behaviour against a
// real httptest server.
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
	var role, label, overflow string
	var visibleCount int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/avatargroup"),
		pageReady(),
		chromedp.Evaluate(`document.getElementById('avatars-demo').getAttribute('role')`, &role),
		chromedp.Evaluate(`document.getElementById('avatars-demo').getAttribute('aria-label')`, &label),
		chromedp.Evaluate(`document.querySelectorAll('#avatars-demo .ui-avatar').length`, &visibleCount),
		chromedp.Evaluate(`document.querySelector('#avatars-demo .ui-avatar-group__overflow').textContent`, &overflow),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if role != "group" {
		t.Errorf("expected role=group, got %q", role)
	}
	if label != "Project team" {
		t.Errorf("expected aria-label='Project team', got %q", label)
	}
	if visibleCount != 4 {
		t.Errorf("expected exactly 4 avatars rendered (Max=4), got %d", visibleCount)
	}
	if !strings.Contains(overflow, "+2") {
		t.Errorf("expected +2 overflow, got %q", overflow)
	}
}

func TestE2E_NewComponents_CopyButton(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var btnPresent, statusPresent bool
	var status string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/copybutton"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('[data-fui-copy-text-from="#copy-source"]') !== null`, &btnPresent),
		chromedp.Evaluate(`document.querySelector('[data-fui-copy-status]') !== null`, &statusPresent),
		chromedp.Evaluate(`document.querySelector('[data-fui-copy-status]')?.getAttribute('role') || ''`, &status),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !btnPresent {
		t.Error("expected copy button to render")
	}
	if !statusPresent {
		t.Error("expected SR-only status sibling to render")
	}
	if status != "status" {
		t.Errorf("expected role=status on copy status span, got %q", status)
	}
}

func TestE2E_NewComponents_ShortcutHintRendersChips(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var kbdCount int
	var srText string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/shortcuthint"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-comp="ui-shortcut-hint"]').length`, &kbdCount),
		chromedp.Evaluate(`document.querySelector('[data-fui-comp="ui-shortcut-hint"] .ui-visually-hidden').textContent`, &srText),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if kbdCount < 3 {
		t.Errorf("expected ≥3 ShortcutHint instances on the demo page, got %d", kbdCount)
	}
	if !strings.Contains(srText, "Shortcut:") {
		t.Errorf("expected SR label starting 'Shortcut:', got %q", srText)
	}
}

func TestE2E_NewComponents_SegmentedControlKeyboard(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var role, initialChecked, afterArrow string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/segmented"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('.ui-segmented[role="radiogroup"]').getAttribute('role')`, &role),
		chromedp.Evaluate(`document.querySelector('.ui-segmented input:checked')?.value || ''`, &initialChecked),
		chromedp.Focus(`.ui-segmented input[type="radio"]:checked`),
		chromedp.KeyEvent("ArrowRight"),
		settle(),
		chromedp.Evaluate(`document.querySelector('.ui-segmented input:checked')?.value || ''`, &afterArrow),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if role != "radiogroup" {
		t.Errorf("expected role=radiogroup, got %q", role)
	}
	if initialChecked != "week" {
		t.Errorf("expected initial Selected=week, got %q", initialChecked)
	}
	if afterArrow == "" {
		t.Errorf("after Arrow press, expected a checked radio, got empty")
	}
}

func TestE2E_NewComponents_ConfirmActionOpensAlertdialog(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var role, ariaModal string
	var modalOpen bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/confirmaction"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('[data-fui-open="demo-confirm-delete"]').click()`, nil),
		settle(),
		chromedp.Evaluate(`document.querySelector('[data-fui-widget="demo-confirm-delete"]')?.getAttribute('role') || ''`, &role),
		chromedp.Evaluate(`document.querySelector('[data-fui-widget="demo-confirm-delete"]')?.getAttribute('aria-modal') || ''`, &ariaModal),
		chromedp.Evaluate(`document.querySelector('[data-fui-widget="demo-confirm-delete"]')?.hasAttribute('hidden') === false`, &modalOpen),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if role != "alertdialog" {
		t.Errorf("expected role=alertdialog, got %q", role)
	}
	if ariaModal != "true" {
		t.Errorf("expected aria-modal=true, got %q", ariaModal)
	}
	if !modalOpen {
		t.Errorf("expected modal to be visible (no hidden attr) after trigger click")
	}
}

func TestE2E_NewComponents_FilterChipBarToolbarRole(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var role, label string
	var chipCount int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/filterchipbar"),
		pageReady(),
		chromedp.Evaluate(`document.getElementById('filter-bar-demo').getAttribute('role')`, &role),
		chromedp.Evaluate(`document.getElementById('filter-bar-demo').getAttribute('aria-label')`, &label),
		chromedp.Evaluate(`document.querySelectorAll('#filter-bar-demo .ui-tag').length`, &chipCount),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if role != "toolbar" {
		t.Errorf("expected role=toolbar, got %q", role)
	}
	if label != "Active filters" {
		t.Errorf("expected aria-label='Active filters', got %q", label)
	}
	if chipCount != 3 {
		t.Errorf("expected 3 chips, got %d", chipCount)
	}
}

func TestE2E_NewComponents_InfiniteScrollFeedRole(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var role, busy string
	var itemCount int
	var rpcPath string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/infinitescroll"),
		pageReady(),
		chromedp.Evaluate(`document.getElementById('feed-demo').getAttribute('role')`, &role),
		chromedp.Evaluate(`document.getElementById('feed-demo').getAttribute('aria-busy')`, &busy),
		chromedp.Evaluate(`document.getElementById('feed-demo').getAttribute('data-fui-infinite-scroll')`, &rpcPath),
		chromedp.Evaluate(`document.querySelectorAll('#feed-demo .demo-feed-item').length`, &itemCount),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if role != "feed" {
		t.Errorf("expected role=feed, got %q", role)
	}
	// aria-busy must settle back to "false" once the (possibly multiple)
	// runtime-driven fetches complete.
	if busy != "false" {
		t.Errorf("expected aria-busy=false after settle, got %q", busy)
	}
	if rpcPath != "/islands/new-components/feed-page" {
		t.Errorf("expected data-fui-infinite-scroll wired, got %q", rpcPath)
	}
	// At least the 5 SSR-rendered items must survive. On a tall enough
	// viewport the runtime auto-fetches further pages (up to 20) — both
	// outcomes are valid; we only care that lazy loading WORKED.
	if itemCount < 5 {
		t.Errorf("expected ≥5 items (SSR floor), got %d", itemCount)
	}
}

func TestE2E_NewComponents_ComboboxARIA(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var role, ariaControls, ariaExpanded string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/combobox"),
		pageReady(),
		chromedp.Evaluate(`document.getElementById('city-combo').getAttribute('role')`, &role),
		chromedp.Evaluate(`document.getElementById('city-combo').getAttribute('aria-controls')`, &ariaControls),
		chromedp.Evaluate(`document.getElementById('city-combo').getAttribute('aria-expanded')`, &ariaExpanded),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if role != "combobox" {
		t.Errorf("expected role=combobox, got %q", role)
	}
	if ariaControls != "city-combo-listbox" {
		t.Errorf("expected aria-controls=city-combo-listbox, got %q", ariaControls)
	}
	if ariaExpanded != "false" {
		t.Errorf("expected aria-expanded=false at first paint, got %q", ariaExpanded)
	}
}

func TestE2E_NewComponents_TreeViewARIA(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var role, label string
	var rootCount int
	var expandedSrc string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/tree"),
		pageReady(),
		chromedp.Evaluate(`document.getElementById('files-tree').getAttribute('role')`, &role),
		chromedp.Evaluate(`document.getElementById('files-tree').getAttribute('aria-label')`, &label),
		chromedp.Evaluate(`document.querySelectorAll('#files-tree > [role="treeitem"]').length`, &rootCount),
		chromedp.Evaluate(`document.getElementById('src').getAttribute('aria-expanded')`, &expandedSrc),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if role != "tree" {
		t.Errorf("expected role=tree, got %q", role)
	}
	if label != "Project files" {
		t.Errorf("expected aria-label='Project files', got %q", label)
	}
	if rootCount != 3 {
		t.Errorf("expected 3 root treeitems (src, vendor, docs), got %d", rootCount)
	}
	if expandedSrc != "true" {
		t.Errorf("expected src node aria-expanded=true, got %q", expandedSrc)
	}
}

func TestE2E_NewComponents_BannerVariantsAndRoles(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var info, warn, danger int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/banner"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-comp="ui-banner"][role="status"]').length`, &info),
		chromedp.Evaluate(`document.querySelectorAll('.ui-banner--warn[role="alert"]').length`, &warn),
		chromedp.Evaluate(`document.querySelectorAll('.ui-banner--danger[role="alert"]').length`, &danger),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if info < 1 {
		t.Errorf("expected ≥1 info/success banner with role=status, got %d", info)
	}
	if warn < 1 {
		t.Errorf("expected ≥1 warn banner with role=alert, got %d", warn)
	}
	if danger < 1 {
		t.Errorf("expected ≥1 danger banner with role=alert, got %d", danger)
	}
}

func TestE2E_NewComponents_TimelineOrderedListWithCurrent(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var olTag, items int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/timeline"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-comp="ui-timeline"]').length`, &olTag),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-comp="ui-timeline"] .ui-timeline__item').length`, &items),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if olTag < 1 {
		t.Errorf("expected ≥1 ui-timeline, got %d", olTag)
	}
	if items < 4 {
		t.Errorf("expected ≥4 timeline events on demo, got %d", items)
	}
}

func TestE2E_NewComponents_StepsAriaCurrent(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var current, complete int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/steps"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-comp="ui-progress-steps"] .ui-progress-steps__item[aria-current="step"]').length`, &current),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-comp="ui-progress-steps"] .ui-progress-steps__item--complete').length`, &complete),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if current < 1 {
		t.Errorf("expected ≥1 step marked aria-current=step, got %d", current)
	}
	if complete < 2 {
		t.Errorf("expected ≥2 completed steps in demo, got %d", complete)
	}
}

func TestE2E_NewComponents_RatingRadioGroup(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var role, label string
	var radios, checked int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/rating"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('[data-fui-comp="ui-rating"]')?.getAttribute('role') || ''`, &role),
		chromedp.Evaluate(`document.querySelector('[data-fui-comp="ui-rating"]')?.getAttribute('aria-label') || ''`, &label),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-comp="ui-rating"] input[type=radio]').length`, &radios),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-comp="ui-rating"] input[type=radio]:checked').length`, &checked),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if role != "radiogroup" {
		t.Errorf("expected role=radiogroup, got %q", role)
	}
	if label == "" {
		t.Errorf("expected aria-label on rating, got empty")
	}
	if radios < 5 {
		t.Errorf("expected ≥5 radios (5-star default), got %d", radios)
	}
	if checked < 1 {
		t.Errorf("expected initial Value to be reflected as one :checked radio, got %d", checked)
	}
}

func TestE2E_NewComponents_ColorPickerNativeInput(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var inputs int
	var labelFor, value string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/colorpicker"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-comp="ui-color-picker"] input[type=color]').length`, &inputs),
		chromedp.Evaluate(`document.querySelector('[data-fui-comp="ui-color-picker"] label')?.getAttribute('for') || ''`, &labelFor),
		chromedp.Evaluate(`document.querySelector('[data-fui-comp="ui-color-picker"] input[type=color]')?.value || ''`, &value),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if inputs < 1 {
		t.Errorf("expected ≥1 native color input, got %d", inputs)
	}
	if labelFor == "" {
		t.Errorf("expected <label for=…> wired to color input")
	}
	if value == "" {
		t.Errorf("expected initial Value to be reflected as input value, got empty")
	}
}

func TestE2E_NewComponents_CommandPaletteOpens(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var role, ariaModal string
	var open bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/commandpalette"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('[data-fui-open="demo-command-palette"]').click()`, nil),
		settle(),
		chromedp.Evaluate(`document.querySelector('[data-fui-widget="demo-command-palette"]')?.getAttribute('role') || ''`, &role),
		chromedp.Evaluate(`document.querySelector('[data-fui-widget="demo-command-palette"]')?.getAttribute('aria-modal') || ''`, &ariaModal),
		chromedp.Evaluate(`document.querySelector('[data-fui-widget="demo-command-palette"]')?.hasAttribute('hidden') === false`, &open),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if role != "dialog" {
		t.Errorf("expected role=dialog, got %q", role)
	}
	if ariaModal != "true" {
		t.Errorf("expected aria-modal=true, got %q", ariaModal)
	}
	if !open {
		t.Errorf("expected palette visible after trigger click")
	}
}

// =============================================================================
// Wave 3 — Tier 1 + Tier 2 static-shape e2e
// =============================================================================

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
	if count < 4 {
		t.Errorf("expected ≥4 Container demo wrappers, got %d", count)
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
		// Click the summary to toggle.
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
	var role, ariaLabel string
	var groups int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/toolbar"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('[data-fui-comp="ui-toolbar"]')?.getAttribute('role') || ''`, &role),
		chromedp.Evaluate(`document.querySelector('[data-fui-comp="ui-toolbar"]')?.getAttribute('aria-label') || ''`, &ariaLabel),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-comp="ui-toolbar"] [role="group"]').length`, &groups),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if role != "toolbar" {
		t.Errorf("expected role=toolbar, got %q", role)
	}
	if ariaLabel == "" {
		t.Errorf("expected aria-label on toolbar")
	}
	if groups < 2 {
		t.Errorf("expected ≥2 role=group children, got %d", groups)
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
	if sparks < 3 {
		t.Errorf("expected ≥3 sparklines on demo, got %d", sparks)
	}
	if paths < 3 {
		t.Errorf("expected ≥3 line paths total, got %d", paths)
	}
}

func TestE2E_PieChart_DonutCenterLabel(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var pies, hasCenter int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/piechart"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-comp="ui-pie-chart"]').length`, &pies),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-comp="ui-pie-chart"] .ui-pie-chart__center-label').length`, &hasCenter),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if pies < 2 {
		t.Errorf("expected pie + donut (2 charts), got %d", pies)
	}
	if hasCenter < 1 {
		t.Errorf("donut should emit center label, got %d", hasCenter)
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
	if bars < 7 {
		t.Errorf("expected ≥7 bars on weekday demo, got %d", bars)
	}
}

func TestE2E_LineChart_MultiSeriesAndLegend(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var lines, legendCircles int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/linechart"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-comp="ui-line-chart"] path.ui-line-chart__line').length`, &lines),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-comp="ui-line-chart"] circle').length`, &legendCircles),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if lines < 3 {
		t.Errorf("expected ≥3 line paths (3 series), got %d", lines)
	}
	if legendCircles < 3 {
		t.Errorf("expected ≥3 legend swatches, got %d", legendCircles)
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
	if nodes < 2 {
		t.Errorf("expected ≥2 collapsible nodes, got %d", nodes)
	}
}

func TestE2E_DiffViewer_BothModesPresent(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var unified, split int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/diffviewer"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-comp="ui-diff-viewer"]:not(.ui-diff-viewer--split)').length`, &unified),
		chromedp.Evaluate(`document.querySelectorAll('.ui-diff-viewer--split').length`, &split),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if unified < 1 {
		t.Errorf("expected ≥1 unified diff viewer, got %d", unified)
	}
	if split < 1 {
		t.Errorf("expected ≥1 split diff viewer, got %d", split)
	}
}

func TestE2E_Markdown_RendersHeadingsAndCode(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var headings, code int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/markdown"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-comp="ui-markdown"] h1').length`, &headings),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-comp="ui-markdown"] pre code').length`, &code),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if headings < 1 {
		t.Errorf("expected ≥1 <h1> from rendered markdown, got %d", headings)
	}
	if code < 1 {
		t.Errorf("expected ≥1 <pre><code> from rendered markdown, got %d", code)
	}
}

// =============================================================================
// Wave 4 — Tier 3 composite & navigation
// =============================================================================

func TestE2E_TOC_PageLoadsAndShellRenders(t *testing.T) {
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
		t.Errorf("expected ≥1 ui-toc nav shell, got %d", navCount)
	}
}

func TestE2E_Lightbox_ThumbsAreAnchors(t *testing.T) {
	// After the Wave-4 follow-up split, Lightbox is standalone and
	// Gallery owns the thumb surface. Triggers now live on
	// [data-fui-comp="ui-gallery"] anchors.
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var n int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/lightbox"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-comp="ui-gallery"] a[data-fui-open]').length`, &n),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if n < 4 {
		t.Errorf("expected ≥4 gallery thumbs wired as lightbox triggers, got %d", n)
	}
}

func TestE2E_NotificationBell_TriggerOpensPopover(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var hiddenAfter string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/notificationbell"),
		pageReady(),
		// Catalog-lazy popover — not in DOM until first opened.
		chromedp.Evaluate(`document.querySelector('[data-fui-comp="ui-notification-bell"]').click()`, nil),
		chromedp.Sleep(600*1e6),
		chromedp.Evaluate(`(document.querySelector('[data-fui-widget="components-bell-demo"]')?.hasAttribute('hidden') ?? null) + ''`, &hiddenAfter),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if hiddenAfter != "false" {
		t.Errorf("clicking the bell should mount + open the popover; after=%q", hiddenAfter)
	}
}

func TestE2E_SortableList_RowsAreDraggable(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var drag, list int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/sortablelist"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-sortable] [data-fui-sortable-item][draggable="true"]').length`, &drag),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-sortable][role="listbox"]').length`, &list),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if drag < 5 {
		t.Errorf("expected ≥5 draggable items, got %d", drag)
	}
	if list < 1 {
		t.Errorf("expected ≥1 sortable listbox, got %d", list)
	}
}

func TestE2E_GlobalSearch_ShortcutWiringPresent(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var sc, tg string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/globalsearch"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('[data-fui-comp="ui-global-search"]')?.getAttribute('data-fui-shortcut-focus') || ''`, &sc),
		chromedp.Evaluate(`document.querySelector('[data-fui-comp="ui-global-search"]')?.getAttribute('data-fui-shortcut-target') || ''`, &tg),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if sc != "/" {
		t.Errorf("expected shortcut focus chord '/', got %q", sc)
	}
	if tg == "" {
		t.Errorf("expected shortcut target selector to be set")
	}
}

func TestE2E_BottomSheet_TriggerOpensBottomMounted(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var hiddenAfter, position string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/bottomsheet"),
		pageReady(),
		// Catalog-lazy widget — not in DOM until first opened.
		chromedp.Evaluate(`document.querySelector('[data-fui-open="components-bottomsheet-demo"]').click()`, nil),
		chromedp.Sleep(600*1e6),
		chromedp.Evaluate(`(document.querySelector('[data-fui-widget="components-bottomsheet-demo"]')?.hasAttribute('hidden') ?? null) + ''`, &hiddenAfter),
		chromedp.Evaluate(`(function(){
			const w = document.querySelector('[data-fui-widget="components-bottomsheet-demo"]');
			if (!w) return '';
			for (const c of w.classList) { if (c.startsWith('fui-pos-')) return c; }
			return '';
		})()`, &position),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if hiddenAfter != "false" {
		t.Errorf("trigger click should open the sheet (hidden=false expected); after=%q", hiddenAfter)
	}
	if position != "fui-pos-bottom" {
		t.Errorf("bottom sheet should mount at bottom; class=%q", position)
	}
}

// TestE2E_BottomSheet_HandleRenders asserts the chrome includes a
// drag-handle bar when the preset enables DragDismiss.
func TestE2E_BottomSheet_HandleRenders(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var handleCount int
	var dragAttr string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/bottomsheet"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('[data-fui-open="components-bottomsheet-demo"]').click()`, nil),
		chromedp.Sleep(600*1e6),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-widget="components-bottomsheet-demo"] [data-fui-drag-handle="true"]').length`, &handleCount),
		chromedp.Evaluate(`document.querySelector('[data-fui-widget="components-bottomsheet-demo"]')?.getAttribute('data-fui-drag-dismiss') || ''`, &dragAttr),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if handleCount != 1 {
		t.Errorf("expected exactly 1 drag handle inside the BottomSheet, got %d", handleCount)
	}
	if dragAttr != "true" {
		t.Errorf("expected data-fui-drag-dismiss=\"true\" on widget root, got %q", dragAttr)
	}
}

// TestE2E_BottomSheet_DragPastThresholdCloses simulates a pointer drag
// that exceeds the 80px distance threshold and asserts the sheet closes.
func TestE2E_BottomSheet_DragPastThresholdCloses(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var hiddenBefore, hiddenAfter string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/bottomsheet"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('[data-fui-open="components-bottomsheet-demo"]').click()`, nil),
		chromedp.Sleep(600*1e6),
		chromedp.Evaluate(`(document.querySelector('[data-fui-widget="components-bottomsheet-demo"]')?.hasAttribute('hidden') ?? null) + ''`, &hiddenBefore),
		chromedp.Evaluate(`(function(){
			const handle = document.querySelector('[data-fui-widget="components-bottomsheet-demo"] [data-fui-drag-handle="true"]');
			if (!handle) return 'no-handle';
			const r = handle.getBoundingClientRect();
			const cx = r.left + r.width / 2;
			const startY = r.top + r.height / 2;
			const opts = { bubbles: true, cancelable: true, pointerId: 1, pointerType: 'touch', clientX: cx, clientY: startY, button: 0 };
			handle.dispatchEvent(new PointerEvent('pointerdown', opts));
			// 4 frames worth of movement crossing the 80px threshold.
			for (let i = 1; i <= 4; i++) {
				const y = startY + i * 30;
				handle.dispatchEvent(new PointerEvent('pointermove', { ...opts, clientY: y }));
			}
			handle.dispatchEvent(new PointerEvent('pointerup', { ...opts, clientY: startY + 120 }));
			return 'ok';
		})()`, nil),
		chromedp.Sleep(500*1e6),
		chromedp.Evaluate(`(function(){
			const w = document.querySelector('[data-fui-widget="components-bottomsheet-demo"]');
			if (!w) return 'gone';
			return w.hasAttribute('hidden') ? 'true' : 'false';
		})()`, &hiddenAfter),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if hiddenBefore != "false" {
		t.Fatalf("precondition: sheet should be open before drag; hidden=%q", hiddenBefore)
	}
	// "Closed" means hidden=true OR removed-from-DOM ("gone") — preset
	// configurations dismiss differently; the test just asserts the
	// drag completed the close path.
	if hiddenAfter == "false" {
		t.Errorf("drag past threshold should close the sheet; after=%q", hiddenAfter)
	}
}

// TestE2E_BottomSheet_ShortDragSnapsBack asserts a drag that doesn't
// cross the distance/velocity thresholds leaves the sheet open and
// clears the live transform.
func TestE2E_BottomSheet_ShortDragSnapsBack(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var hiddenAfter, transform string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/bottomsheet"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('[data-fui-open="components-bottomsheet-demo"]').click()`, nil),
		chromedp.Sleep(600*1e6),
		chromedp.Evaluate(`(function(){
			const handle = document.querySelector('[data-fui-widget="components-bottomsheet-demo"] [data-fui-drag-handle="true"]');
			if (!handle) return 'no-handle';
			const r = handle.getBoundingClientRect();
			const cx = r.left + r.width / 2;
			const startY = r.top + r.height / 2;
			const opts = { bubbles: true, cancelable: true, pointerId: 1, pointerType: 'touch', clientX: cx, clientY: startY, button: 0 };
			handle.dispatchEvent(new PointerEvent('pointerdown', opts));
			handle.dispatchEvent(new PointerEvent('pointermove', { ...opts, clientY: startY + 20 }));
			// Release before crossing the 80px distance threshold and at a
			// gentle velocity so the snap-back path runs.
			return new Promise(resolve => setTimeout(() => {
				handle.dispatchEvent(new PointerEvent('pointerup', { ...opts, clientY: startY + 20 }));
				resolve('ok');
			}, 200));
		})()`, nil),
		chromedp.Sleep(300*1e6),
		chromedp.Evaluate(`(document.querySelector('[data-fui-widget="components-bottomsheet-demo"]')?.hasAttribute('hidden') ?? null) + ''`, &hiddenAfter),
		chromedp.Evaluate(`document.querySelector('[data-fui-widget="components-bottomsheet-demo"]')?.style.transform || ''`, &transform),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if hiddenAfter != "false" {
		t.Errorf("short drag should NOT close the sheet; hidden=%q", hiddenAfter)
	}
	if transform != "" {
		t.Errorf("snap-back should clear the live transform; got %q", transform)
	}
}

// =============================================================================
// Wave 4 follow-up: Lightbox split + Gallery + Carousel
// =============================================================================

func TestE2E_Gallery_ItemsAreAnchors(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var n int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/gallery"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-comp="ui-gallery"] a').length`, &n),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if n < 6 {
		t.Errorf("expected ≥6 gallery anchors across demos, got %d", n)
	}
}

func TestE2E_Gallery_LightboxTriggersHaveDeeplink(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var n int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/lightbox"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-comp="ui-gallery"] a[data-fui-open="components-lightbox-demo"][data-fui-deeplink]').length`, &n),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if n < 4 {
		t.Errorf("expected ≥4 gallery anchors wired to the lightbox, got %d", n)
	}
}

func TestE2E_Lightbox_ClickArrowsCycleImages(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var firstSrc, afterNext, afterPrev string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/lightbox"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('[data-fui-comp="ui-gallery"] a[data-fui-open]').click()`, nil),
		chromedp.Sleep(700*1e6),
		chromedp.Evaluate(`document.querySelector('[data-fui-widget="components-lightbox-demo"] img.ui-lightbox__full')?.getAttribute('src') || ''`, &firstSrc),
		chromedp.Evaluate(`document.querySelector('[data-fui-lightbox-next]').click()`, nil),
		chromedp.Sleep(400*1e6),
		chromedp.Evaluate(`document.querySelector('[data-fui-widget="components-lightbox-demo"] img.ui-lightbox__full')?.getAttribute('src') || ''`, &afterNext),
		chromedp.Evaluate(`document.querySelector('[data-fui-lightbox-prev]').click()`, nil),
		chromedp.Sleep(400*1e6),
		chromedp.Evaluate(`document.querySelector('[data-fui-widget="components-lightbox-demo"] img.ui-lightbox__full')?.getAttribute('src') || ''`, &afterPrev),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if firstSrc == "" {
		t.Fatalf("expected first image src to populate; got empty")
	}
	if afterNext == firstSrc {
		t.Errorf("Next button should change src; before=%q after=%q", firstSrc, afterNext)
	}
	if afterPrev != firstSrc {
		t.Errorf("Prev after Next should return to first src; got %q want %q", afterPrev, firstSrc)
	}
}

// TestE2E_Lightbox_PinchScalesImage simulates a 2-pointer pinch-out on
// the displayed image and asserts the runtime sets data-fui-zoomed and
// applies a scale transform.
func TestE2E_Lightbox_PinchScalesImage(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var zoomedAttr, transform string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/lightbox"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('[data-fui-comp="ui-gallery"] a[data-fui-open]').click()`, nil),
		chromedp.Sleep(700*1e6),
		chromedp.Evaluate(`(function(){
			const img = document.querySelector('[data-fui-widget="components-lightbox-demo"] img.ui-lightbox__full');
			if (!img) return 'no-img';
			const r = img.getBoundingClientRect();
			const cx = r.left + r.width/2, cy = r.top + r.height/2;
			const off = 40;
			const opts = (id, x, y) => ({ bubbles: true, cancelable: true, pointerId: id, pointerType: 'touch', clientX: x, clientY: y, button: 0 });
			// Two pointers down at distance=80
			img.dispatchEvent(new PointerEvent('pointerdown', opts(1, cx - off, cy)));
			img.dispatchEvent(new PointerEvent('pointerdown', opts(2, cx + off, cy)));
			// Move them apart to ~240 → 3× scale.
			img.dispatchEvent(new PointerEvent('pointermove', opts(1, cx - off*3, cy)));
			img.dispatchEvent(new PointerEvent('pointermove', opts(2, cx + off*3, cy)));
			// Release.
			img.dispatchEvent(new PointerEvent('pointerup', opts(1, cx - off*3, cy)));
			img.dispatchEvent(new PointerEvent('pointerup', opts(2, cx + off*3, cy)));
			return 'ok';
		})()`, nil),
		chromedp.Sleep(200*1e6),
		chromedp.Evaluate(`document.querySelector('[data-fui-widget="components-lightbox-demo"] img.ui-lightbox__full')?.getAttribute('data-fui-zoomed') !== null ? 'yes' : 'no'`, &zoomedAttr),
		chromedp.Evaluate(`document.querySelector('[data-fui-widget="components-lightbox-demo"] img.ui-lightbox__full')?.style.transform || ''`, &transform),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if zoomedAttr != "yes" {
		t.Errorf("expected data-fui-zoomed marker after pinch-out; got %q", zoomedAttr)
	}
	if !strings.Contains(transform, "scale(") {
		t.Errorf("expected scale() in transform after pinch; got %q", transform)
	}
}

// TestE2E_ModulePreload_PopoverPageHasPopoverLink verifies that the
// server-side runtime-module dependency scan emits a
// <link rel="modulepreload"> tag in <head> for the popover module on
// a page that uses popover-anchored widgets.
func TestE2E_ModulePreload_PopoverPageHasPopoverLink(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var head string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/popover"),
		pageReady(),
		chromedp.Evaluate(`document.head.innerHTML`, &head),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !strings.Contains(head, `rel="modulepreload"`) {
		t.Fatalf("expected at least one <link rel=modulepreload> on a popover page; got head:\n%s", head)
	}
	if !strings.Contains(head, "/__gofastr/runtime/popover.js") {
		t.Errorf("expected preload for popover.js on /components/popover; got head:\n%s", head)
	}
	if !strings.Contains(head, "/__gofastr/runtime/widgets.js") {
		t.Errorf("expected preload for widgets.js (popover opens a widget); got head:\n%s", head)
	}
}

// TestE2E_ModulePreload_BarePageHasNoModuleLinks asserts the scanner
// doesn't emit preload tags for pages without any demand-load markers
// — every preload is a wasted RTT if the module isn't actually used.
func TestE2E_ModulePreload_BarePageHasNoModuleLinks(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var head string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/breadcrumbs"), // pure SSR, no JS modules
		pageReady(),
		chromedp.Evaluate(`document.head.innerHTML`, &head),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if strings.Contains(head, "/__gofastr/runtime/popover.js") {
		t.Errorf("breadcrumbs page should not preload popover.js; got head:\n%s", head)
	}
	if strings.Contains(head, "/__gofastr/runtime/lightbox.js") {
		t.Errorf("breadcrumbs page should not preload lightbox.js; got head:\n%s", head)
	}
}

// TestE2E_Carousel_VirtualHydratesOnScroll asserts that placeholder
// slides past the initial window are empty at first paint and get
// hydrated with real HTML after the user scrolls them into view.
func TestE2E_Carousel_VirtualHydratesOnScroll(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var initialDeferred, finalDeferred int
	var lastSlideHydrated bool
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/carousel"),
		pageReady(),
		chromedp.Sleep(300*1e6),
		chromedp.Evaluate(`document.querySelectorAll('#demo-virtual-carousel [data-fui-carousel-defer]').length`, &initialDeferred),
		// Scroll the track in steps from the Go side so the
		// IntersectionObserver fires for the slides crossing into view
		// on each tick. A single jump-to-end would leave the middle
		// slides un-hydrated.
		chromedp.ActionFunc(func(ctx context.Context) error {
			// Step through the full scroll width in half-viewport chunks.
			// Each step gives the IntersectionObserver time to fire for
			// slides that just crossed into the read-ahead window before
			// the next jump.
			for step := 0; step < 120; step++ {
				if err := chromedp.Evaluate(fmt.Sprintf(`(function(){
					const tr = document.querySelector('#demo-virtual-carousel [data-fui-carousel-track]');
					if (!tr) return 'no-track';
					const target = %d * tr.clientWidth * 0.5;
					if (target > tr.scrollWidth) return 'done';
					tr.scrollTo({ left: target, behavior: 'auto' });
					return 'ok';
				})()`, step), nil).Do(ctx); err != nil {
					return err
				}
				if err := chromedp.Sleep(40 * 1e6).Do(ctx); err != nil {
					return err
				}
			}
			return nil
		}),
		chromedp.Sleep(400*1e6),
		chromedp.Evaluate(`document.querySelectorAll('#demo-virtual-carousel [data-fui-carousel-defer]').length`, &finalDeferred),
		chromedp.Evaluate(`(function(){
			const last = document.querySelector('#demo-virtual-carousel [data-fui-carousel-slide="59"]');
			return !!(last && last.querySelector('svg'));
		})()`, &lastSlideHydrated),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if initialDeferred < 50 {
		t.Errorf("expected most slides deferred at first paint; got %d", initialDeferred)
	}
	if finalDeferred != 0 {
		t.Errorf("scrolling to end should hydrate all placeholders; %d still deferred", finalDeferred)
	}
	if !lastSlideHydrated {
		t.Errorf("last slide should be hydrated (have <svg> inside) after full scroll")
	}
}

// TestE2E_Lightbox_PinchResetsOnClose asserts the zoom transform clears
// when the lightbox modal closes.
func TestE2E_Lightbox_PinchResetsOnClose(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var transformAfterClose, zoomedAfterClose string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/lightbox"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('[data-fui-comp="ui-gallery"] a[data-fui-open]').click()`, nil),
		chromedp.Sleep(700*1e6),
		chromedp.Evaluate(`(function(){
			const img = document.querySelector('[data-fui-widget="components-lightbox-demo"] img.ui-lightbox__full');
			if (!img) return 'no-img';
			const r = img.getBoundingClientRect();
			const cx = r.left + r.width/2, cy = r.top + r.height/2;
			const opts = (id, x, y) => ({ bubbles: true, cancelable: true, pointerId: id, pointerType: 'touch', clientX: x, clientY: y, button: 0 });
			img.dispatchEvent(new PointerEvent('pointerdown', opts(1, cx - 40, cy)));
			img.dispatchEvent(new PointerEvent('pointerdown', opts(2, cx + 40, cy)));
			img.dispatchEvent(new PointerEvent('pointermove', opts(1, cx - 120, cy)));
			img.dispatchEvent(new PointerEvent('pointermove', opts(2, cx + 120, cy)));
			img.dispatchEvent(new PointerEvent('pointerup', opts(1, cx - 120, cy)));
			img.dispatchEvent(new PointerEvent('pointerup', opts(2, cx + 120, cy)));
			return 'ok';
		})()`, nil),
		chromedp.Sleep(200*1e6),
		chromedp.KeyEvent(kb.Escape),
		chromedp.Sleep(400*1e6),
		// Re-open and inspect the (same) image instance.
		chromedp.Evaluate(`document.querySelector('[data-fui-comp="ui-gallery"] a[data-fui-open]').click()`, nil),
		chromedp.Sleep(500*1e6),
		chromedp.Evaluate(`document.querySelector('[data-fui-widget="components-lightbox-demo"] img.ui-lightbox__full')?.style.transform || ''`, &transformAfterClose),
		chromedp.Evaluate(`document.querySelector('[data-fui-widget="components-lightbox-demo"] img.ui-lightbox__full')?.getAttribute('data-fui-zoomed') !== null ? 'yes' : 'no'`, &zoomedAfterClose),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if transformAfterClose != "" {
		t.Errorf("close-then-reopen should clear transform; got %q", transformAfterClose)
	}
	if zoomedAfterClose != "no" {
		t.Errorf("close-then-reopen should clear data-fui-zoomed; got %q", zoomedAfterClose)
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

func TestE2E_Carousel_DotClickJumpsToSlide(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var x1, x2 float64
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/carousel"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('[data-fui-carousel] [data-fui-carousel-track]').scrollLeft`, &x1),
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-carousel] [data-fui-carousel-dot]')[2].click()`, nil),
		chromedp.Sleep(600*1e6),
		chromedp.Evaluate(`document.querySelector('[data-fui-carousel] [data-fui-carousel-track]').scrollLeft`, &x2),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if x2 <= x1 {
		t.Errorf("clicking dot 3 should scroll forward; before=%f after=%f", x1, x2)
	}
}
