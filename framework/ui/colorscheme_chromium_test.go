package ui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// A ColorScheme subtree re-scopes the palette, the native-control scheme
// and the compiled component options: a dark band on a light page paints
// the dark background, its primary button the dark primary, and a light
// card nested back inside it (or on a dark page) the light values.
func TestColorSchemeSubtreeOverride(t *testing.T) {
	if testing.Short() {
		t.Skip("browser test disabled in short mode")
	}
	th := theme.Default()
	css := th.CSSCustomProperties() + buttonStyle.Entry().CSSFor(th)
	button := string(Button(ButtonConfig{Label: "Go", Variant: ButtonPrimary}))
	body := string(ColorScheme(SchemeDark,
		render.HTML(`<span id="dark-btn">`+button+`</span><input id="dark-input">`),
		ColorScheme(SchemeLight, render.HTML(`<span id="nested-btn">`+button+`</span>`)),
	))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><html data-color-scheme="%s"><meta charset=utf-8>
<style>%s</style><style>*{transition:none!important}</style><body>%s</body>`,
			r.URL.Query().Get("page"), css, body)
	}))
	t.Cleanup(srv.Close)
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))

	// rgb resolves a theme value the way the browser paints it.
	rgb := func(v string) string {
		var out string
		if err := chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(
			`(() => { const p = document.createElement('i'); p.style.color = %q; document.body.append(p); const c = getComputedStyle(p).color; p.remove(); return c; })()`, v), &out)); err != nil {
			t.Fatal(err)
		}
		return out
	}
	probe := func(sel, prop string) string {
		var out string
		if err := chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(`getComputedStyle(document.querySelector(%q))[%q]`, sel, prop), &out)); err != nil {
			t.Fatal(err)
		}
		return out
	}
	wrapper := `[data-color-scheme="dark"]:not(html)`
	nested := `[data-color-scheme="light"]:not(html)`

	if err := chromedp.Run(ctx, chromedp.Navigate(srv.URL+"/?page=light")); err != nil {
		t.Fatal(err)
	}
	checks := []struct{ what, got, want string }{
		{"the light page's color-scheme", probe("html", "colorScheme"), "light"},
		{"the dark band's background", probe(wrapper, "backgroundColor"), rgb(th.DarkColors["background"])},
		{"the dark band's color-scheme", probe(wrapper, "colorScheme"), "dark"},
		{"an input's color-scheme inside the dark band", probe("#dark-input", "colorScheme"), "dark"},
		{"the dark band's primary button", probe("#dark-btn .fui-button", "backgroundColor"), rgb(th.DarkColors["primary"])},
		{"the light card's background inside the dark band", probe(nested, "backgroundColor"), rgb(th.Colors.Background.Value)},
		{"the light card's primary button", probe("#nested-btn .fui-button", "backgroundColor"), rgb(th.Colors.Primary.Value)},
	}
	if err := chromedp.Run(ctx, chromedp.Navigate(srv.URL+"/?page=dark")); err != nil {
		t.Fatal(err)
	}
	checks = append(checks,
		struct{ what, got, want string }{"the dark page's color-scheme", probe("html", "colorScheme"), "dark"},
		struct{ what, got, want string }{"a light card on the dark page", probe(nested, "backgroundColor"), rgb(th.Colors.Background.Value)},
		struct{ what, got, want string }{"a light card's color-scheme on the dark page", probe(nested, "colorScheme"), "light"},
	)
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.what, c.got, c.want)
		}
	}
}
