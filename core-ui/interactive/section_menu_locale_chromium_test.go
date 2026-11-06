//go:build chromium

package interactive

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

func TestMenuFilterKeepsLocaleDigits(t *testing.T) {
	cfg := sampleMenu()
	cfg.Ctx = i18n.WithContext(context.Background(), i18n.Locale{Tag: "ar-EG"})
	cfg.Filter = true
	cfg.Groups = []SectionGroup{{Label: "Group", Items: []SectionItem{
		{Label: "Alpha", Href: "/alpha"}, {Label: "Beta", Href: "/beta"},
	}}}
	body := `<script>document.documentElement.lang = 'ar-EG';</script>` + SectionMenu(cfg).String()
	ctx := chromedptest.Context(t)
	var filtered, cleared string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(menuPage(t, body, true)),
		chromedp.Evaluate(`(() => { const input = document.querySelector('.cui-section-menu__filter-input'); input.value = 'Alpha'; input.dispatchEvent(new Event('input')); return document.querySelector('.cui-section-menu__count').textContent; })()`, &filtered),
		chromedp.Evaluate(`(() => { const input = document.querySelector('.cui-section-menu__filter-input'); input.value = ''; input.dispatchEvent(new Event('input')); return document.querySelector('.cui-section-menu__count').textContent; })()`, &cleared),
	); err != nil {
		t.Fatal(err)
	}
	if filtered != "١/٢" || cleared != "٢" {
		t.Fatalf("filtered = %q, cleared = %q; want Arabic digits ١/٢ and ٢", filtered, cleared)
	}
}
