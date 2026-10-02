package ui

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
)

// TestCardRowHoverTintsOnlyInteractiveRows pins the hover gate: the row
// variant's hover tint once applied to every row card, telling the user
// a plain (unlinkable) record was clickable. The tint belongs to
// .fui-card--interactive, the class linked rows carry.
func TestCardRowHoverTintsOnlyInteractiveRows(t *testing.T) {
	linked := Card(CardConfig{Variant: CardRow, Href: "/x", Heading: "LINK"}, render.Text("badge"))
	plain := Card(CardConfig{Variant: CardRow, Heading: "PLAIN"}, render.Text("badge"))
	css := cardStyle.Entry().CSSFor(theme.Default())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><meta charset=utf-8>
<style>*,*::before,*::after{box-sizing:border-box}body{margin:0;padding:24px;display:grid;gap:8px}
%s
%s</style>%s%s`,
			theme.Default().CSSCustomProperties(), css, string(linked), string(plain))
	}))
	t.Cleanup(srv.Close)

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(),
		append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.WSURLReadTimeout(90*time.Second),
			chromedp.NoSandbox)...)
	t.Cleanup(cancelAlloc)
	ctx, cancel := chromedp.NewContext(allocCtx)
	ctx, cancelTimeout := context.WithTimeout(ctx, 60*time.Second)
	t.Cleanup(cancelTimeout)
	t.Cleanup(cancel)

	const measure = `(() => {
	const rows = [...document.querySelectorAll('.fui-card--row')];
	const pick = t => rows.find(e => e.textContent.includes(t));
	const out = {};
	for (const t of ['LINK', 'PLAIN']) {
		const e = pick(t), b = e.getBoundingClientRect();
		out[t] = { hover: e.matches(':hover'), bg: getComputedStyle(e).backgroundColor, cx: b.x + b.width / 2, cy: b.y + b.height / 2 };
	}
	return out;
})()`
	var row map[string]any
	// Hover the PLAIN row first: it must not pick up the tint.
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL),
		chromedp.EmulateViewport(1280, 800),
		chromedp.Evaluate(`(() => { const b = [...document.querySelectorAll('.fui-card--row')].find(e => e.textContent.includes('PLAIN')).getBoundingClientRect(); return {x: b.x + b.width/2, y: b.y + b.height/2}; })()`, &row),
		chromedp.ActionFunc(func(ctx context.Context) error {
			return input.DispatchMouseEvent(input.MouseMoved, row["x"].(float64), row["y"].(float64)).Do(ctx)
		}),
		chromedp.Evaluate(measure, &row),
	); err != nil {
		t.Fatal(err)
	}
	plainRow := row["PLAIN"].(map[string]any)
	if !plainRow["hover"].(bool) {
		t.Fatal("pointer is not over the plain row; the check measured nothing")
	}
	if plainRow["bg"].(string) == "rgba(0, 0, 0, 0.04)" || plainRow["bg"].(string) == "rgb(39, 39, 42)" {
		t.Errorf("plain row tints on hover (%v) — the tint must be gated on .fui-card--interactive", plainRow["bg"])
	}
	// Then the linked row: it keeps the tint.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`(() => { const b = [...document.querySelectorAll('.fui-card--row')].find(e => e.textContent.includes('LINK')).getBoundingClientRect(); return {x: b.x + b.width/2, y: b.y + b.height/2}; })()`, &row),
		chromedp.ActionFunc(func(ctx context.Context) error {
			return input.DispatchMouseEvent(input.MouseMoved, row["x"].(float64), row["y"].(float64)).Do(ctx)
		}),
		chromedp.Evaluate(measure, &row),
	); err != nil {
		t.Fatal(err)
	}
	linkedRow := row["LINK"].(map[string]any)
	if !linkedRow["hover"].(bool) {
		t.Fatal("pointer is not over the linked row; the check measured nothing")
	}
	if linkedRow["bg"].(string) == "rgba(0, 0, 0, 0)" {
		t.Errorf("linked row lost its hover tint (bg %v)", linkedRow["bg"])
	}
}
