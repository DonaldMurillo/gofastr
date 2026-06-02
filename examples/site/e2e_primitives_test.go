package main

import (
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// =============================================================================
// E2E contract tests for primitives ported from examples/website.
//
// Dropped: layout (/components/layout does not exist in site),
// toggle (site calls it "switch"; covered by e2e_test.go),
// popover (site has no /components/popover slug).
//
// Kept: card, image, tooltip, tag, spinner, divider, fileupload.
// =============================================================================

// ─── Card ───────────────────────────────────────────────────────────

func TestE2E_Card_LabelledByHeading(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var labelledBy, role string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/card"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('[data-fui-comp="ui-card"]')?.getAttribute('aria-labelledby') || ''`, &labelledBy),
		chromedp.Evaluate(`document.querySelector('[data-fui-comp="ui-card"]')?.getAttribute('role') || ''`, &role),
	); err != nil {
		t.Fatalf("card: %v", err)
	}
	if !strings.HasPrefix(labelledBy, "ui-card-") {
		t.Errorf("card aria-labelledby = %q, want ui-card-*", labelledBy)
	}
	if role != "region" {
		t.Errorf("card role = %q, want region", role)
	}
}

func TestE2E_Card_InteractiveIsAnchor(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var tag, href string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/card"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('a[data-fui-comp="ui-card"]')?.tagName || ''`, &tag),
		chromedp.Evaluate(`document.querySelector('a[data-fui-comp="ui-card"]')?.getAttribute('href') || ''`, &href),
	); err != nil {
		t.Fatalf("card interactive: %v", err)
	}
	if tag != "A" {
		t.Errorf("interactive card tag = %q, want A", tag)
	}
	if href == "" {
		t.Errorf("interactive card must have href")
	}
}

// ─── OptimizedImage ─────────────────────────────────────────────────

func TestE2E_OptimizedImage_HasWidthHeightAndLazyLoading(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var w, h, loading, decoding string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/image"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('.ui-image__img')?.getAttribute('width') || ''`, &w),
		chromedp.Evaluate(`document.querySelector('.ui-image__img')?.getAttribute('height') || ''`, &h),
		chromedp.Evaluate(`document.querySelector('.ui-image__img')?.getAttribute('loading') || ''`, &loading),
		chromedp.Evaluate(`document.querySelector('.ui-image__img')?.getAttribute('decoding') || ''`, &decoding),
	); err != nil {
		t.Fatalf("image: %v", err)
	}
	if w == "" || h == "" {
		t.Errorf("image must have width+height for CLS: w=%q h=%q", w, h)
	}
	if loading != "lazy" {
		t.Errorf("image loading = %q, want lazy", loading)
	}
	if decoding != "async" {
		t.Errorf("image decoding = %q, want async", decoding)
	}
}

// ─── Tooltip ────────────────────────────────────────────────────────

func TestE2E_Tooltip_TriggerHasAriaDescribedBy(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var describedBy, popRole string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/tooltip"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('[data-fui-comp="ui-tooltip"] button')?.getAttribute('aria-describedby') || ''`, &describedBy),
		chromedp.Evaluate(`document.querySelector('.ui-tooltip__pop')?.getAttribute('role') || ''`, &popRole),
	); err != nil {
		t.Fatalf("tooltip: %v", err)
	}
	if describedBy == "" {
		t.Errorf("tooltip trigger should carry aria-describedby")
	}
	if popRole != "tooltip" {
		t.Errorf("pop role = %q, want tooltip", popRole)
	}
}

// ─── Tag ────────────────────────────────────────────────────────────

func TestE2E_Tag_DismissButtonHasAccessibleLabel(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var ariaLabel, rpcPath string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/tag"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('.ui-tag__dismiss')?.getAttribute('aria-label') || ''`, &ariaLabel),
		chromedp.Evaluate(`document.querySelector('.ui-tag__dismiss')?.getAttribute('data-fui-rpc') || ''`, &rpcPath),
	); err != nil {
		t.Fatalf("tag: %v", err)
	}
	if !strings.HasPrefix(ariaLabel, "Remove ") {
		t.Errorf("dismiss aria-label = %q, want 'Remove …'", ariaLabel)
	}
	if rpcPath == "" {
		t.Errorf("dismiss button should carry data-fui-rpc")
	}
}

// ─── Spinner ────────────────────────────────────────────────────────

func TestE2E_Spinner_HasStatusRoleAndAriaBusy(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var role, ariaBusy, hiddenLabel string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/spinner"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('[data-fui-comp="ui-spinner"]')?.getAttribute('role') || ''`, &role),
		chromedp.Evaluate(`document.querySelector('[data-fui-comp="ui-spinner"]')?.getAttribute('aria-busy') || ''`, &ariaBusy),
		chromedp.Evaluate(`document.querySelector('[data-fui-comp="ui-spinner"] .ui-visually-hidden')?.textContent || ''`, &hiddenLabel),
	); err != nil {
		t.Fatalf("spinner: %v", err)
	}
	if role != "status" {
		t.Errorf("role = %q, want status", role)
	}
	if ariaBusy != "true" {
		t.Errorf("aria-busy = %q, want true", ariaBusy)
	}
	if !strings.Contains(hiddenLabel, "Loading") {
		t.Errorf("expected screen-reader label containing 'Loading', got %q", hiddenLabel)
	}
}

// ─── Divider ────────────────────────────────────────────────────────

func TestE2E_Divider_PlainUsesHR(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var tag string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/divider"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('hr[data-fui-comp="ui-divider"]')?.tagName || ''`, &tag),
	); err != nil {
		t.Fatalf("divider: %v", err)
	}
	if tag != "HR" {
		t.Errorf("plain divider should render <hr>, got %q", tag)
	}
}

// ─── FileUpload ─────────────────────────────────────────────────────

func TestE2E_FileUpload_NativeInputAndDropZone(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var inputType, accept string
	var hasDropZone bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/fileupload"),
		pageReady(),
		// Site demo: name="avatar", accept="image/*"
		chromedp.Evaluate(`document.querySelector('input[name="avatar"]')?.getAttribute('type') || ''`, &inputType),
		chromedp.Evaluate(`document.querySelector('input[name="avatar"]')?.getAttribute('accept') || ''`, &accept),
		chromedp.Evaluate(`document.querySelector('[data-fui-fileupload]') !== null`, &hasDropZone),
	); err != nil {
		t.Fatalf("fileupload: %v", err)
	}
	if inputType != "file" {
		t.Errorf("input type = %q, want file", inputType)
	}
	if accept == "" {
		t.Errorf("expected accept attribute to be passed through")
	}
	if !hasDropZone {
		t.Errorf("expected data-fui-fileupload drop zone marker")
	}
}

func TestE2E_FileUpload_PreviewShowsFilename(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var preview string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/fileupload"),
		pageReady(),
		chromedp.Evaluate(`(function(){
			const input = document.querySelector('input[name="avatar"]');
			if (!input) return '';
			const dt = new DataTransfer();
			dt.items.add(new File(["img"], "photo.jpg", {type: "image/jpeg"}));
			input.files = dt.files;
			input.dispatchEvent(new Event('change', {bubbles: true}));
			// Give the runtime a tick to update the preview.
			return '';
		})()`, nil),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('[data-fui-comp="ui-fileupload"] .ui-fileupload__filename')?.textContent || ''`, &preview),
	); err != nil {
		t.Fatalf("fileupload preview: %v", err)
	}
	if !strings.Contains(preview, "photo.jpg") {
		t.Errorf("filename preview should contain photo.jpg; got %q", preview)
	}
}
