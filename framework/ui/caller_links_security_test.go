package ui_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/chromedp/chromedp"
)

func callerLinks(href string) render.HTML {
	return ui.ColumnPicker(ui.ColumnPickerConfig{
		ID: "columns", Label: "Columns", ResetHref: href, ResetLabel: "Reset",
		Columns: []ui.ColumnChoice{{Label: "Title", Shown: true, ToggleHref: href, UpHref: href, DownHref: href}},
	}) + ui.ActionList(ui.ActionListConfig{Items: []ui.ActionListItem{{Label: "Open", Href: href}}}) +
		ui.SegmentedLinks(ui.SegmentedLinksConfig{Label: "Layout", Items: []ui.SegmentLink{{Text: "Table", Href: href, Current: true}}})
}

func TestCallerLinksRefuseUnsafeURLs(t *testing.T) {
	for _, href := range []string{"javascript:alert(1)", "JaVaScRiPt:alert(2)", "data:text/html,unsafe", "vbscript:unsafe", "//evil.example/x", "/\\evil.example/x", "/path\x00", "/path%0aunsafe"} {
		t.Run(href, func(t *testing.T) {
			markup := string(callerLinks(href))
			if strings.Contains(markup, "href=") {
				t.Fatalf("refused URL still renders a live anchor: %q", href)
			}
			if count := strings.Count(markup, `aria-disabled="true"`); count != 6 {
				t.Fatalf("disabled controls = %d, want 6", count)
			}
		})
	}
	for _, href := range []string{"/columns?shown=title", "?view=table", "#details", "https://example.com/path", "mailto:help@example.com", "tel:+15551234567"} {
		if count := strings.Count(string(callerLinks(href)), `href="`+href+`"`); count != 6 {
			t.Errorf("safe URL %q retained by %d controls, want 6", href, count)
		}
	}
}

func TestCallerLinksDisableUnsafeInBrowser(t *testing.T) {
	server := themeTestPageWithHead(t, "", string(callerLinks("javascript:alert(1)")))
	ctx := moduleTestCtxURL(t, server.URL)
	var valid bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		const controls = document.querySelectorAll('.fui-colpick__toggle,.fui-colpick__move,.fui-colpick__reset,.fui-action-list__item,.fui-seglinks__item');
		return controls.length === 6 && [...controls].every(control => control.tagName === 'SPAN' && control.getAttribute('aria-disabled') === 'true' && !control.hasAttribute('href'));
	})()`, &valid)); err != nil {
		t.Fatal(err)
	}
	if !valid {
		t.Fatal("unsafe caller links remain navigable")
	}
}
