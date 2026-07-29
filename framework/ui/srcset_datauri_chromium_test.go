//go:build chromium

package ui

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// Settles, against a real browser, whether a comma inside an
// otherwise whitespace-free srcset candidate splits that candidate.
//
// The srcset grammar collects a URL as "a sequence of code points that
// are not ASCII whitespace" — commas included — and only strips commas
// that TRAIL the token. MDN's "must not contain commas" is authoring
// advice, not the parse algorithm. This test exists because that
// distinction decides whether encodeSrcsetURL must percent-encode the
// comma in a data: URI, and percent-encoding it corrupts base64.
func TestSrcsetDataURICommaInBrowser(t *testing.T) {
	// A 1x1 transparent GIF. The comma after "base64" is the delimiter
	// under test: if the browser splits there, currentSrc comes back as
	// the bare media type and the image never decodes.
	const gif = "data:image/gif;base64,R0lGODlhAQABAIAAAP///wAAACH5BAEAAAAALAAAAAABAAEAAAICRAEAOw=="

	page := fmt.Sprintf(`<!doctype html><meta charset=utf-8>
<img id=a srcset="%s 1x" width=1>`, gif)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, page)
	}))
	defer srv.Close()

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(),
		append(chromedp.DefaultExecAllocatorOptions[:], chromedp.NoSandbox)...)
	defer cancelAlloc()
	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()
	ctx, cancelTimeout := context.WithTimeout(ctx, 30*time.Second)
	defer cancelTimeout()

	var currentSrc string
	var naturalWidth int
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL),
		chromedp.WaitVisible("#a", chromedp.ByID),
		chromedp.Evaluate(`document.getElementById('a').currentSrc`, &currentSrc),
		chromedp.Evaluate(`document.getElementById('a').naturalWidth`, &naturalWidth),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}

	if currentSrc != gif {
		t.Fatalf("browser split the candidate at the data URI's comma:\n got  %q\n want %q",
			currentSrc, gif)
	}
	if naturalWidth != 1 {
		t.Fatalf("selected candidate did not decode as an image (naturalWidth=%d, currentSrc=%q)",
			naturalWidth, currentSrc)
	}
}

// The adversarial reading of the same relaxation: if a comma DID split a
// candidate, a data: URI whose payload is an absolute URL would smuggle a
// second, attacker-controlled candidate into the set. It does not — the
// whole thing stays one URL token, so the only thing the browser can try
// to load is an undecodable data: URI.
func TestSrcsetDataURIPayloadIsNotASecondCandidate(t *testing.T) {
	const smuggle = "data:image/gif;base64,https://evil.example/tracker.png"

	page := fmt.Sprintf(`<!doctype html><meta charset=utf-8>
<img id=a srcset="%s 10w" sizes="10px" width=1>`, smuggle)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, page)
	}))
	defer srv.Close()

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(),
		append(chromedp.DefaultExecAllocatorOptions[:], chromedp.NoSandbox)...)
	defer cancelAlloc()
	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()
	ctx, cancelTimeout := context.WithTimeout(ctx, 30*time.Second)
	defer cancelTimeout()

	var currentSrc string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL),
		chromedp.WaitReady("#a", chromedp.ByID),
		chromedp.Evaluate(`document.getElementById('a').currentSrc`, &currentSrc),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}

	if currentSrc != smuggle {
		t.Fatalf("comma split the candidate — the payload became its own source:\n got  %q\n want %q",
			currentSrc, smuggle)
	}
}
