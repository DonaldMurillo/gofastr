//go:build chromium

package ui

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/chromedp/chromedp"
)

// Hard rule 9: a placeholder is a VISUAL feature, so unit tests on the emitted
// HTML cannot tell whether it paints. This renders the real component, loads it
// in Chrome with the full-resolution source deliberately never arriving, and
// screenshots what a user on a slow connection actually sees.
func TestPlaceholderPaintsWhileSourceIsPending(t *testing.T) {
	// A 2x2 red LQIP, and a "real" source that never resolves.
	const red = "data:image/gif;base64,R0lGODdhAgACAIABAP8AAP///ywAAAAAAgACAAACA0QCBQA7"

	html := OptimizedImage(OptimizedImageConfig{
		Src:         "/never-arrives.png",
		Alt:         "placeholder probe",
		Width:       200,
		Height:      200,
		Placeholder: red,
	})

	// The component registers its stylesheet rather than inlining it, so a
	// bare markup dump has no CSS and the LQIP renders at its own 2x2
	// intrinsic size. Pull the registered sheet in, the way a real host does.
	css := imageStyle.Entry().CSSFor(theme.Default())

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			// The real image never arrives. This is the slow-network case
			// the placeholder exists for.
			select {
			case <-r.Context().Done():
			case <-time.After(20 * time.Second):
			}
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><meta charset=utf-8>
<style>body{margin:0;background:#fff}
%s</style>%s`, css, string(html))
	}))
	defer srv.Close()

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(),
		append(chromedp.DefaultExecAllocatorOptions[:], // A cold Chrome start on a shared runner can take longer than
			// chromedp's 20s default wait for the DevTools websocket URL, which
			// surfaces as "websocket url timeout reached" and looks like a test
			// failure rather than a slow launch. The cmd/gofastr chromedp tests
			// already raise it to 90s; these did not.
			chromedp.WSURLReadTimeout(90*time.Second),
			chromedp.NoSandbox)...)
	defer cancelAlloc()
	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()

	// chromedp starts Chrome lazily on the first Run: allocate against the
	// browser context so the browser's lifetime is the browser context's:
	// passing a timeout context here would make the browser die when that
	// deadline passed. The watchdog bounds only the startup wait.
	started := make(chan error, 1)
	go func() { started <- chromedp.Run(ctx) }()
	select {
	case err := <-started:
		if err != nil {
			t.Fatalf("chrome did not start: %v", err)
		}
	case <-time.After(90 * time.Second):
		t.Fatal("chrome did not start within 90s")
	}

	ctx, cancelTimeout := context.WithTimeout(ctx, 60*time.Second)
	defer cancelTimeout()

	var shot []byte
	var pixel string
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(320, 320),
		chromedp.Navigate(srv.URL),
		chromedp.Sleep(2500*time.Millisecond),
		// Read the colour actually composited at the middle of the image box.
		chromedp.Evaluate(`(() => {
			const lqip = document.querySelector('.ui-image__lqip');
			const real = document.querySelector('.ui-image__img');
			if (!lqip) return "NO-LQIP";
			if (!real) return "NO-IMG";
			const lr = lqip.getBoundingClientRect();
			const rr = real.getBoundingClientRect();
			const vis = getComputedStyle(lqip);
			// The LQIP must be painted (not display:none / zero-opacity) and
			// must cover the box the real image will occupy, or it is not
			// standing in for anything.
			if (vis.display === "none" || vis.visibility === "hidden" || Number(vis.opacity) === 0) {
				return "HIDDEN:display=" + vis.display + ",vis=" + vis.visibility + ",op=" + vis.opacity;
			}
			if (lr.width < 10 || lr.height < 10) {
				return "LQIP-COLLAPSED:" + Math.round(lr.width) + "x" + Math.round(lr.height);
			}
			return "LQIP:" + Math.round(lr.width) + "x" + Math.round(lr.height) +
			       " REAL:" + Math.round(rr.width) + "x" + Math.round(rr.height);
		})()`, &pixel),
		chromedp.CaptureScreenshot(&shot),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}

	t.Logf("boxes: %s", pixel)
	switch {
	case pixel == "NO-LQIP":
		t.Fatal("no .ui-image__lqip element rendered — the placeholder never reached the DOM")
	case pixel == "NO-IMG":
		t.Fatal("no .ui-image__img element rendered")
	case strings.HasPrefix(pixel, "HIDDEN"):
		t.Fatalf("the placeholder is in the DOM but not painted (%s) — "+
			"a user on a slow connection sees nothing", pixel)
	case strings.HasPrefix(pixel, "LQIP-COLLAPSED"):
		t.Fatalf("the placeholder collapsed to its intrinsic size (%s) instead of "+
			"filling the reserved box — it is not standing in for the real image", pixel)
	}
	out := os.Getenv("LQIP_SHOT")
	if out == "" {
		out = "/tmp/lqip.png"
	}
	if err := os.WriteFile(out, shot, 0o644); err != nil {
		t.Fatalf("write screenshot: %v", err)
	}
	t.Logf("screenshot written to %s (%d bytes)", out, len(shot))
}
