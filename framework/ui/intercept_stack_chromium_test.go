//go:build chromium

package ui

import (
	"bytes"
	"context"
	"fmt"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
)

// interceptStackPage draws the markup the intercept module mounts for
// a stack with one layer per presentation in as: the overlay host with
// one child per layer, each wearing the presentation the server chose
// for it, every layer but the top inert. Each layer holds a record
// header and its details, the shape an entity record drawer shows.
func interceptStackPage(as ...string) string {
	var b strings.Builder
	b.WriteString(string(PageHeader(PageHeaderConfig{Title: "Invoices", Subtitle: "Every invoice, newest first"})))
	b.WriteString(`<div id="cui-intercept" data-cui-intercept-overlay>`)
	recs := []string{"INV-0042", "Acme Corp", "SUB-0007", "PAY-0193"}
	for i, a := range as {
		rec := recs[i]
		attrs := ""
		if i < len(as)-1 {
			attrs = ` inert aria-hidden="true"`
		}
		fmt.Fprintf(&b, `<div class="layer" data-cui-intercept-as="%s"%s>`, a, attrs)
		b.WriteString(string(PageHeader(PageHeaderConfig{Title: rec, Eyebrow: fmt.Sprintf("Layer %d", i+1), HeadingLevel: 2})))
		b.WriteString(string(DetailList(DetailListConfig{Items: []DetailItem{
			{Label: "Status", Value: "Open"},
			{Label: "Amount", Value: "$1,200.00"},
			{Label: "Issued", Value: "2026-10-01"},
		}})))
		b.WriteString(`</div>`)
	}
	b.WriteString(`</div>`)
	return b.String()
}

type interceptLayerBox struct{ L, R, T, B, W float64 }

// TestInterceptDrawersStackOverlapped: stacked drawers sit docked to the
// inline end over each other, each a step narrower than the layer under
// it, and each lower layer's visible strip is darker than the one above
// it (every layer casts the scrim over the layers under it); below the
// drawer breakpoint every layer is a full-width sheet at the bottom
// edge.
const interceptStackProbe = `(() => {
	const vw = document.documentElement.clientWidth, vh = document.documentElement.clientHeight;
	const ls = Array.from(document.querySelectorAll('#cui-intercept > .layer')).map(el => {
		const r = el.getBoundingClientRect();
		return {L: r.left, R: r.right, T: r.top, B: r.bottom, W: r.width};
	});
	return {VW: vw, VH: vh, Layers: ls};
})()`

type interceptStackBoxes struct {
	VW, VH float64
	Layers []interceptLayerBox
}

// interceptStackBrowser serves interceptStackPage(as...) under the
// overlay CSS and the theme, and opens it at 1280x800 with reduced
// motion: the enter animation is a translate, so without it the boxes
// would be measured mid-flight.
func interceptStackBrowser(t *testing.T, as ...string) context.Context {
	t.Helper()
	css := theme.Default().CSSCustomProperties() +
		pageHeaderStyle.Entry().CSSFor(theme.Default()) +
		detailListStyle.Entry().CSSFor(theme.Default()) +
		app.InterceptOverlayCSS() +
		`*,*::before,*::after{box-sizing:border-box}body{margin:0;padding:16px;background:var(--color-bg);color:var(--color-text)}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><html data-color-scheme="light"><head><meta charset=utf-8><meta name=viewport content="width=device-width"><style>%s</style></head><body>%s</body></html>`,
			css, interceptStackPage(as...))
	}))
	t.Cleanup(srv.Close)

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(),
		append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.WSURLReadTimeout(90*time.Second),
			chromedp.NoSandbox, chromedp.WindowSize(1280, 800))...)
	t.Cleanup(cancelAlloc)
	ctx, cancel := chromedp.NewContext(allocCtx)
	t.Cleanup(cancel)
	ctx, cancelTimeout := context.WithTimeout(ctx, 60*time.Second)
	t.Cleanup(cancelTimeout)
	if err := chromedp.Run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			return emulation.SetEmulatedMedia().
				WithFeatures([]*emulation.MediaFeature{{Name: "prefers-reduced-motion", Value: "reduce"}}).
				Do(ctx)
		}),
		chromedp.Navigate(srv.URL),
	); err != nil {
		t.Fatal(err)
	}
	return ctx
}

func TestInterceptDrawersStackOverlapped(t *testing.T) {
	ctx := interceptStackBrowser(t, "drawer", "drawer", "drawer", "drawer")
	const probe = interceptStackProbe
	var got interceptStackBoxes
	var shot []byte
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(probe, &got),
		chromedp.CaptureScreenshot(&shot),
	); err != nil {
		t.Fatal(err)
	}
	if len(got.Layers) != 4 {
		t.Fatalf("want 4 layers, got %d", len(got.Layers))
	}
	for i, l := range got.Layers {
		if l.R != got.VW || l.T != 0 || l.B != got.VH {
			t.Errorf("layer %d is not docked full-height at the inline end: %+v (viewport %.0fx%.0f)", i+1, l, got.VW, got.VH)
		}
		if i > 0 && got.Layers[i-1].W-l.W != 32 {
			t.Errorf("layer %d is %.0fpx wide, want one 32px step under layer %d's %.0fpx", i+1, l.W, i, got.Layers[i-1].W)
		}
	}
	if got.Layers[0].W != 480 {
		t.Errorf("the bottom drawer is %.0fpx wide, want the 480px default", got.Layers[0].W)
	}

	// Pixels: sample each layer's visible strip (between its own inline
	// start and the next layer's) low in the drawer, below any text, and
	// the top layer's surface. Deeper is darker, one step per layer.
	img, err := png.Decode(bytes.NewReader(shot))
	if err != nil {
		t.Fatal(err)
	}
	luma := func(x, y float64) int {
		r, g, b, _ := img.At(int(x), int(y)).RGBA()
		return int((299*(r>>8) + 587*(g>>8) + 114*(b>>8)) / 1000)
	}
	y := got.VH - 40
	var lum [4]int
	for i, l := range got.Layers {
		lum[i] = luma(l.L+8, y)
	}
	for i := 1; i < 4; i++ {
		if lum[i-1] >= lum[i] {
			t.Errorf("layer %d's strip (luma %d) is not darker than layer %d's (luma %d): every layer must dim the layers under it", i, lum[i-1], i+1, lum[i])
		}
	}
	interceptStackShots(t, ctx, "")

	if err := chromedp.Run(ctx, chromedp.EmulateViewport(390, 844), chromedp.Evaluate(probe, &got)); err != nil {
		t.Fatal(err)
	}
	for i, l := range got.Layers {
		if l.L != 0 || l.W != got.VW || l.B != got.VH {
			t.Errorf("phone layer %d is not a full-width sheet at the bottom edge: %+v (viewport %.0fx%.0f)", i+1, l, got.VW, got.VH)
		}
	}
	interceptStackShots(t, ctx, "-mobile")
}

// interceptStackShots saves light and dark screenshots when
// INTERCEPT_SHOT_DIR names a directory, for the eyes-on-the-pixels
// pass; without it the test asserts geometry and luma only.
func interceptStackShots(t *testing.T, ctx context.Context, suffix string) {
	t.Helper()
	dir := os.Getenv("INTERCEPT_SHOT_DIR")
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, scheme := range []string{"light", "dark"} {
		var buf []byte
		if err := chromedp.Run(ctx,
			chromedp.Evaluate(`document.documentElement.setAttribute('data-color-scheme', '`+scheme+`')`, nil),
			chromedp.CaptureScreenshot(&buf),
		); err != nil {
			t.Fatal(err)
		}
		name := filepath.Join(dir, "intercept-stack-"+scheme+suffix+".png")
		if err := os.WriteFile(name, buf, 0o600); err != nil {
			t.Fatal(err)
		}
		t.Logf("screenshot: %s", name)
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.documentElement.setAttribute('data-color-scheme', 'light')`, nil)); err != nil {
		t.Fatal(err)
	}
}

// TestInterceptMixedStackDocksEachLayer: each layer docks by its own
// presentation, so sheets over a drawer overlap at the bottom edge,
// full width, and the drawer under them stays docked at the inline end.
func TestInterceptMixedStackDocksEachLayer(t *testing.T) {
	ctx := interceptStackBrowser(t, "drawer", "sheet", "sheet")
	var got interceptStackBoxes
	if err := chromedp.Run(ctx, chromedp.Evaluate(interceptStackProbe, &got)); err != nil {
		t.Fatal(err)
	}
	if len(got.Layers) != 3 {
		t.Fatalf("want 3 layers, got %d", len(got.Layers))
	}
	d := got.Layers[0]
	if d.R != got.VW || d.T != 0 || d.B != got.VH || d.W != 480 {
		t.Errorf("the drawer under the sheets is not docked full-height at the inline end, 480px wide: %+v", d)
	}
	for i, l := range got.Layers[1:] {
		if l.L != 0 || l.W != got.VW || l.B != got.VH || l.T <= 0 {
			t.Errorf("sheet %d is not a full-width layer at the bottom edge: %+v (viewport %.0fx%.0f)", i+1, l, got.VW, got.VH)
		}
	}
	interceptStackShots(t, ctx, "-mixed")
}
