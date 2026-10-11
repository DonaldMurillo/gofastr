//go:build chromium

package ui

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/chromedp/chromedp"
)

// formFramePage is one record form drawn through the frame, wrapped in
// a width-bounded box: the demo-stage shape, so the frame's container
// query has a real column to live in.
func formFramePage(wide bool) render.HTML {
	// The wide box lifts the form's default measure (--ui-form-max,
	// 42rem) the way a record page does, so the frame has the width a
	// two-column record form is drawn at; the narrow box is drawer-wide.
	box := `class="narrow-box"`
	if wide {
		box = `class="wide-box" style="--ui-form-max: none"`
	}
	return render.HTML(`<div `+box+`>`) + formFrameDemoForm() + render.HTML(`</div>`)
}

// formFrameDemoForm composes the record shape: bulk fields in Main,
// short controllers in Side.
func formFrameDemoForm() render.HTML {
	return Form(FormConfig{Action: "#", ID: "ff-demo", HideSubmit: false, SubmitLabel: "Save"},
		FormFrame(FormFrameConfig{
			Main: []render.HTML{
				TextField(TextFieldConfig{Name: "number", Label: "Number", ID: "ff-number", Value: "INV-0042"}),
				TextField(TextFieldConfig{Name: "memo", Label: "Memo", ID: "ff-memo", Placeholder: "What this invoice is for"}),
			},
			Side: []render.HTML{
				Select(SelectConfig{Name: "status", Label: "Status", ID: "ff-status",
					Options: []SelectOption{{Value: "open", Text: "Open"}, {Value: "paid", Text: "Paid"}}}),
			},
		}))
}

// sits beside the main one when the frame is wide and under it when
// the frame is narrow, on the SAME viewport, because the switch reads
// the frame's own width (a container query) and not the window's.
func TestFormFrameSideDropsUnderOnItsOwnWidth(t *testing.T) {
	css := formFrameStyle.Entry().CSSFor(theme.Default()) +
		formStyle.Entry().CSSFor(theme.Default()) +
		formFieldStyle.Entry().CSSFor(theme.Default()) +
		selectStyle.Entry().CSSFor(theme.Default()) +
		buttonStyle.Entry().CSSFor(theme.Default()) +
		theme.Default().CSSCustomProperties() +
		`*,*::before,*::after{box-sizing:border-box}body{margin:0;padding:16px;display:grid;gap:24px;justify-items:start}
		 .wide-box{inline-size:64rem} .narrow-box{inline-size:30rem}`

	page := string(formFramePage(true)) + string(formFramePage(false))
	probe := `(() => {
		const box = c => document.querySelector('.' + c + ' [data-cui-comp="ui-form-frame"]');
		const cols = f => {
			const main = f.querySelector('.fui-form-frame__main').getBoundingClientRect();
			const side = f.querySelector('.fui-form-frame__side').getBoundingClientRect();
			return {mainL: main.left, mainR: main.right, mainT: main.top,
				sideL: side.left, sideT: side.top, sideW: side.width,
				frameW: f.getBoundingClientRect().width};
		};
		return {wide: cols(box('wide-box')), narrow: cols(box('narrow-box'))};
	})()`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><html><head><meta charset=utf-8><style>%s</style></head><body>%s</body></html>`,
			css, page)
	}))
	defer srv.Close()

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(),
		append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.WSURLReadTimeout(90*time.Second),
			chromedp.NoSandbox, chromedp.WindowSize(1400, 900))...)
	defer cancelAlloc()
	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()
	ctx, cancelTimeout := context.WithTimeout(ctx, 60*time.Second)
	defer cancelTimeout()

	var got struct {
		Wide, Narrow struct {
			MainL, MainR, MainT, SideL, SideT, SideW, FrameW float64
		}
	}
	if err := chromedp.Run(ctx, chromedp.Navigate(srv.URL), chromedp.Evaluate(probe, &got)); err != nil {
		t.Fatal(err)
	}

	// Wide frame: the side column sits BESIDE the main one — same top
	// edge, starting after the main column ends — and keeps its rail
	// width rather than a share of the row.
	w := got.Wide
	if w.SideT != w.MainT {
		t.Errorf("wide frame: side top %.0f != main top %.0f — the columns are not side by side", w.SideT, w.MainT)
	}
	if w.SideL < w.MainR {
		t.Errorf("wide frame: side starts at %.0f, before the main column ends at %.0f", w.SideL, w.MainR)
	}
	if w.SideW < 200 || w.SideW > 280 {
		t.Errorf("wide frame: side column is %.0fpx wide, want the ~16rem rail (256px)", w.SideW)
	}

	// Narrow frame, same viewport: the side column sits UNDER the main
	// one, at the frame's own left edge, sharing its width.
	n := got.Narrow
	if n.SideT <= n.MainT {
		t.Errorf("narrow frame: side top %.0f is not below main top %.0f — the rail never dropped under", n.SideT, n.MainT)
	}
	if n.SideL > n.MainL+1 {
		t.Errorf("narrow frame: side starts at %.0f, past the main column's left edge %.0f", n.SideL, n.MainL)
	}
	if n.FrameW < 470 || n.FrameW > 490 {
		t.Errorf("narrow frame: the frame is %.0fpx wide, want the 30rem (480px) box", n.FrameW)
	}

	formFrameShots(t, ctx, srv.URL)
}

// formFrameShots saves a light and a dark screenshot of the two frames
// when FORMFRAME_SHOT_DIR names a directory, for the report's
// eyes-on-the-pixels pass; without it the test asserts geometry only.
func formFrameShots(t *testing.T, ctx context.Context, url string) {
	t.Helper()
	dir := os.Getenv("FORMFRAME_SHOT_DIR")
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		scheme, name string
	}{
		{"light", "formframe-light.png"},
		{"dark", "formframe-dark.png"},
	} {
		var buf []byte
		if err := chromedp.Run(ctx,
			chromedp.Evaluate(`document.documentElement.setAttribute('data-color-scheme', '`+tc.scheme+`')`, nil),
			chromedp.FullScreenshot(&buf, 90),
		); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, tc.name), buf, 0o600); err != nil {
			t.Fatal(err)
		}
		t.Logf("screenshot: %s", filepath.Join(dir, tc.name))
	}
}
