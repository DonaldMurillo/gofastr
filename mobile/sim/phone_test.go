// Package sim drives the notes app in a Chromium set up as a phone
// WebView: iPhone-sized viewport, touch, mobile user agent, and EVERY
// request intercepted through the DevTools Fetch domain and answered by
// mobilehost in-process. Chromium never opens a network connection,
// which is exactly the iOS/Android shell's position (WKURLSchemeHandler /
// shouldInterceptRequest). What the simulation cannot show: the native
// platforms' own interception limits (no redirects, no cookies, see
// README), streamed SSE (CDP's fulfill is buffered), and real WebKit.
package sim

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/framework/owner"

	"github.com/DonaldMurillo/gofastr-mobile/examples/notes/notesapp"
	"github.com/DonaldMurillo/gofastr-mobile/mobilehost"
)

const origin = mobilehost.DefaultOrigin

const iPhoneUA = "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148"

type stats struct {
	served, sse, blocked atomic.Int64
	mu                   sync.Mutex
	errs                 []string
	paths                []string
}

func (s *stats) note(format string, a ...any) {
	s.mu.Lock()
	s.errs = append(s.errs, fmt.Sprintf(format, a...))
	s.mu.Unlock()
}

// serve answers one paused request from the in-process host.
func serve(ctx context.Context, h *mobilehost.Host, st *stats, ev *fetch.EventRequestPaused) {
	exec := cdp.WithExecutor(ctx, chromedp.FromContext(ctx).Target)
	u, err := url.Parse(ev.Request.URL + ev.Request.URLFragment)
	if err != nil || !strings.HasPrefix(ev.Request.URL, origin+"/") && ev.Request.URL != origin {
		// Anything off-origin is what an offline shell would refuse.
		st.blocked.Add(1)
		st.note("blocked off-origin request %s", ev.Request.URL)
		_ = fetch.FailRequest(ev.RequestID, network.ErrorReasonBlockedByClient).Do(exec)
		return
	}
	if u.Path == "/__gofastr/sse" {
		// CDP fulfills with a complete body; a held stream cannot be
		// simulated here (mobilehost.Open covers it in Go tests).
		st.sse.Add(1)
		_ = fetch.FailRequest(ev.RequestID, network.ErrorReasonAborted).Do(exec)
		return
	}
	hdr := http.Header{}
	for k, v := range ev.Request.Headers {
		hdr.Set(k, fmt.Sprint(v))
	}
	var body []byte
	for _, e := range ev.Request.PostDataEntries {
		b, err := base64.StdEncoding.DecodeString(e.Bytes)
		if err != nil {
			st.note("bad post data on %s: %v", u.Path, err)
		}
		body = append(body, b...)
	}
	if os.Getenv("SIM_ANDROID_LIMITS") == "1" {
		// Android's WebResourceRequest carries no body: reproduce that to
		// see what a shell without a body bridge breaks.
		body = nil
	}
	if ev.Request.HasPostData && len(ev.Request.PostDataEntries) == 0 {
		st.note("post data omitted by CDP on %s", u.Path)
	}
	rctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	resp, err := h.Do(rctx, &mobilehost.Request{Method: ev.Request.Method, URL: ev.Request.URL, Header: hdr, Body: body})
	if err != nil {
		st.note("host error on %s %s: %v", ev.Request.Method, u.Path, err)
		_ = fetch.FailRequest(ev.RequestID, network.ErrorReasonFailed).Do(exec)
		return
	}
	st.served.Add(1)
	st.mu.Lock()
	st.paths = append(st.paths, fmt.Sprintf("%s %s -> %d", ev.Request.Method, u.RequestURI(), resp.Status))
	st.mu.Unlock()
	var entries []*fetch.HeaderEntry
	for k, vs := range resp.Header {
		for _, v := range vs {
			entries = append(entries, &fetch.HeaderEntry{Name: k, Value: v})
		}
	}
	if err := fetch.FulfillRequest(ev.RequestID, int64(resp.Status)).
		WithResponseHeaders(entries).
		WithBody(base64.StdEncoding.EncodeToString(resp.Body)).Do(exec); err != nil {
		st.note("fulfill %s: %v", u.Path, err)
	}
}

func shotsDir(t *testing.T) string {
	if d := os.Getenv("SIM_SHOTS"); d != "" {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
		return d
	}
	return t.TempDir()
}

func TestPhoneWebView(t *testing.T) {
	exe := os.Getenv("SIM_CHROME")
	if exe == "" {
		for _, p := range []string{"/opt/pw-browsers/chromium-1243/chrome-linux64/chrome", "/usr/bin/google-chrome"} {
			if _, err := os.Stat(p); err == nil {
				exe = p
				break
			}
		}
	}
	if exe == "" {
		t.Skip("no Chromium found; set SIM_CHROME")
	}
	t.Cleanup(func() { owner.SetExtractor(nil) })

	_, h, err := notesapp.Build(t.TempDir(), origin)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = h.Stop(ctx)
	}()

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(),
		append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.ExecPath(exe), chromedp.NoSandbox, chromedp.Headless,
			// Belt and braces: even a request the interceptor missed has
			// nowhere to go.
			chromedp.ProxyServer("http://127.0.0.1:9"),
			// A loaded CI box can take longer than chromedp's default
			// start waits to bring Chrome up (internal/chromedptest's
			// launch-allowance gate; that helper is internal, so this
			// module spells the two allowances itself).
			chromedp.WSURLReadTimeout(60*time.Second),
		)...)
	defer cancelAlloc()
	ctx, cancel := chromedp.NewContext(allocCtx, chromedp.WithBrowserOption(chromedp.WithDialTimeout(60*time.Second)))
	defer cancel()
	ctx, cancelT := context.WithTimeout(ctx, 90*time.Second)
	defer cancelT()

	st := &stats{}
	chromedp.ListenTarget(ctx, func(ev any) {
		switch e := ev.(type) {
		case *fetch.EventRequestPaused:
			go serve(ctx, h, st, e)
		case *cdpruntime.EventExceptionThrown:
			st.note("page exception: %s", e.ExceptionDetails.Error())
		case *cdpruntime.EventConsoleAPICalled:
			if e.Type == cdpruntime.APITypeError {
				var parts []string
				for _, a := range e.Args {
					parts = append(parts, string(a.Value)+a.Description)
				}
				st.note("console.error: %s", strings.Join(parts, " "))
			}
		}
	})

	dir := shotsDir(t)
	shot := func(name string) chromedp.Action {
		return chromedp.ActionFunc(func(ctx context.Context) error {
			var buf []byte
			if err := chromedp.CaptureScreenshot(&buf).Do(ctx); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(dir, name+".png"), buf, 0o600)
		})
	}
	var marker, bodyText, path string
	err = chromedp.Run(ctx,
		fetch.Enable().WithPatterns([]*fetch.RequestPattern{{URLPattern: "*", RequestStage: fetch.RequestStageRequest}}),
		emulation.SetDeviceMetricsOverride(390, 844, 3, true),
		emulation.SetTouchEmulationEnabled(true),
		emulation.SetUserAgentOverride(iPhoneUA),
		chromedp.Navigate(origin+"/"),
		chromedp.WaitVisible(`a[href="/notes/create"]`, chromedp.ByQuery),
		shot("01-list-empty-light"),
		// Client-side navigation: a window marker set now must survive
		// the click, or the router fell back to a full load.
		chromedp.Evaluate(`window.__simMarker = "kept"`, nil),
		chromedp.Click(`a[href="/notes/create"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`input[name="title"]`, chromedp.ByQuery),
		chromedp.Evaluate(`window.__simMarker || "lost"`, &marker),
		shot("02-create-light"),
		chromedp.SendKeys(`input[name="title"]`, "Groceries", chromedp.ByQuery),
		chromedp.SendKeys(`textarea[name="body"]`, "Milk, eggs, coffee", chromedp.ByQuery),
		chromedp.Click(`button[type="submit"]`, chromedp.ByQuery),
		chromedp.Poll(`!location.pathname.endsWith("/create") && document.body.innerText.includes("Groceries")`, nil, chromedp.WithPollingTimeout(15*time.Second)),
		chromedp.Location(&path),
		shot("03-after-save-light"),
		chromedp.Navigate(origin+"/notes"),
		chromedp.WaitVisible(`main`, chromedp.ByQuery),
		chromedp.Text(`body`, &bodyText, chromedp.ByQuery),
		shot("04-list-light"),
		emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{{Name: "prefers-color-scheme", Value: "dark"}}),
		chromedp.Sleep(300*time.Millisecond),
		shot("05-list-dark"),
		chromedp.Click(`a[href="/notes/create"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`input[name="title"]`, chromedp.ByQuery),
		shot("06-create-dark"),
	)
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, p := range st.paths {
		t.Log(p)
	}
	for _, e := range st.errs {
		t.Log("NOTE:", e)
	}
	t.Logf("served in-process: %d, sse aborted: %d, blocked off-origin: %d, after save at %s, shots in %s",
		st.served.Load(), st.sse.Load(), st.blocked.Load(), path, dir)
	if err != nil {
		t.Fatalf("phone flow: %v", err)
	}
	if marker != "kept" {
		t.Errorf("navigation to /notes/create was a full page load, not a client-side swap")
	}
	if !strings.Contains(bodyText, "Groceries") {
		t.Errorf("the saved note is not in the list")
	}
	if st.blocked.Load() != 0 {
		t.Errorf("the page tried to reach %d off-origin URLs", st.blocked.Load())
	}
}
