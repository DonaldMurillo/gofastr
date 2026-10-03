package runtime

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// ─── SSE on demand ──────────────────────────────────────────────────
//
// The stream opens only while the live document holds a push target
// (an island, or the offline banner that reads the mirrored connection
// state) and closes when the last one leaves; the availability meta
// alone opens nothing. These tests watch the CDP network log, not the
// DOM: the whole contract is which /__gofastr/sse requests exist.

// sseNetLog records every /__gofastr/sse request the page makes and
// which of them the browser reported finished (loadingFinished or
// loadingFailed — the latter is how a client-closed EventSource shows
// up). Attach with chromedp.ListenTarget BEFORE the navigate.
type sseNetLog struct {
	mu       sync.Mutex
	opened   []string          // request URLs, in order
	reqURL   map[string]string // requestID → URL
	finished map[string]string // requestID → URL, once finished
}

func newSSENetLog() *sseNetLog {
	return &sseNetLog{reqURL: map[string]string{}, finished: map[string]string{}}
}

func (l *sseNetLog) listen(ctx context.Context) {
	chromedp.ListenTarget(ctx, func(ev interface{}) {
		switch e := ev.(type) {
		case *network.EventRequestWillBeSent:
			if strings.Contains(e.Request.URL, "/__gofastr/sse") {
				l.mu.Lock()
				l.opened = append(l.opened, e.Request.URL)
				l.reqURL[string(e.RequestID)] = e.Request.URL
				l.mu.Unlock()
			}
		case *network.EventLoadingFinished:
			l.mark(string(e.RequestID))
		case *network.EventLoadingFailed:
			l.mark(string(e.RequestID))
		}
	})
}

func (l *sseNetLog) mark(id string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if u, ok := l.reqURL[id]; ok {
		l.finished[id] = u
	}
}

func (l *sseNetLog) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.opened)
}

func (l *sseNetLog) finishedCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.finished)
}

// sseDemandServer serves the runtime, the split modules, an SSE stream
// that hangs open (so a stray open would be visible), and two pages:
// "/" with the meta and (unless plain) an island, "/plain" with the
// meta and no push target. The /plain partial answer carries
// X-Gofastr-Session: sess-2, the rollover a real navigation delivers
// when the server re-minted, so the reopen on the way back must use it.
func sseDemandServer(t *testing.T) *httptest.Server {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	mux.HandleFunc("/__gofastr/sse", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		fl, _ := w.(http.Flusher)
		fmt.Fprint(w, ": connected\n\n")
		if fl != nil {
			fl.Flush()
		}
		<-r.Context().Done()
	})
	page := func(body string) string {
		return `<!doctype html><html><head>` +
			`<meta name="gofastr-sse" content="/__gofastr/sse?session=sess-1">` +
			`<script type="application/json" id="gofastr-routes">[{"path":"/"},{"path":"/plain"}]</script>` +
			`</head><body><main role="main" tabindex="-1">` + body + `</main>` +
			`<script src="/__gofastr/runtime.js"></script></body></html>`
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.Header.Get("X-Gofastr-Navigate") == "1" {
			// Partial answer for the SPA nav back to "/" — served with
			// the CURRENT session so a fresh fetch also rolls the meta.
			w.Header().Set("X-Gofastr-Partial", "true")
			w.Header().Set("X-Gofastr-Session", "sess-2")
			fmt.Fprint(w, `<div data-island="live"><span id="home">Home</span></div><a id="to-plain" href="/plain">plain</a>`)
			return
		}
		fmt.Fprint(w, page(`<div data-island="live"><span id="home">Home</span></div><a id="to-plain" href="/plain">plain</a>`))
	})
	mux.HandleFunc("/plain", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.Header.Get("X-Gofastr-Navigate") == "1" {
			w.Header().Set("X-Gofastr-Partial", "true")
			w.Header().Set("X-Gofastr-Session", "sess-2")
			fmt.Fprint(w, `<h1 id="plain">Plain</h1><a id="to-home" href="/">home</a>`)
			return
		}
		fmt.Fprint(w, page(`<h1 id="plain">Plain</h1><a id="to-home" href="/">home</a>`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// TestSSEClosedOnPageWithoutPushTargets: a page with the availability
// meta but no island, presence roster or connection banner must make
// NO /__gofastr/sse request — the meta says SSE exists, not "open it".
func TestSSEClosedOnPageWithoutPushTargets(t *testing.T) {
	srv := sseDemandServer(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	log := newSSENetLog()
	log.listen(ctx)

	if err := chromedp.Run(ctx,
		network.Enable(),
		// The plain page: meta, no push target anywhere.
		chromedp.Navigate(srv.URL+"/plain"),
		chromedp.WaitVisible(`#plain`, chromedp.ByID),
		// The module loader defers idle loads (requestIdleCallback /
		// setTimeout(0)); give the idle slot and any would-be connect
		// time to fire before judging.
		chromedp.Sleep(1500*time.Millisecond),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if got := log.count(); got != 0 {
		t.Fatalf("page without push targets made %d /__gofastr/sse request(s) (%v) — the availability meta must not open the stream", got, log.opened)
	}
}

// TestSSEOpensForIsland: a page with an island opens the stream — and
// opens it ONCE (no duplicated EventSource per scan).
func TestSSEOpensForIsland(t *testing.T) {
	srv := sseDemandServer(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	log := newSSENetLog()
	log.listen(ctx)

	if err := chromedp.Run(ctx,
		network.Enable(),
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#home`, chromedp.ByID),
		// The module idle-loads, then connects.
		chromedp.Poll(`window.__gofastr && window.__gofastr.sseStatus && window.__gofastr.sseStatus.connected === true`,
			nil, chromedp.WithPollingTimeout(15*time.Second), chromedp.WithPollingInterval(100*time.Millisecond)),
		// Settle: a duplicate open (one per rescan) would land here.
		chromedp.Sleep(1500*time.Millisecond),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if got := log.count(); got != 1 {
		t.Fatalf("island page opened %d /__gofastr/sse request(s), want exactly 1: %v", got, log.opened)
	}
	if !strings.Contains(log.opened[0], "session=sess-1") {
		t.Fatalf("stream opened with the wrong session id: %v", log.opened)
	}
}

// TestSSEClosesAndReopensAcrossNavigation: island page → plain page
// closes the stream (the browser reports the SSE request finished; no
// new one appears) → back to the island page opens a NEW stream, and
// the session parameter is the CURRENT one: the plain page's answer
// carried X-Gofastr-Session: sess-2, the rollover that rewrites the
// meta, so the reopen must connect with sess-2, not the stale sess-1.
func TestSSEClosesAndReopensAcrossNavigation(t *testing.T) {
	srv := sseDemandServer(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	log := newSSENetLog()
	log.listen(ctx)

	if err := chromedp.Run(ctx,
		network.Enable(),
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#home`, chromedp.ByID),
		chromedp.Poll(`window.__gofastr && window.__gofastr.sseStatus && window.__gofastr.sseStatus.connected === true`,
			nil, chromedp.WithPollingTimeout(15*time.Second), chromedp.WithPollingInterval(100*time.Millisecond)),
	); err != nil {
		t.Fatalf("chromedp (island page): %v", err)
	}
	if got := log.count(); got != 1 {
		t.Fatalf("island page opened %d stream(s), want 1: %v", got, log.opened)
	}

	// Navigate to the page with no push targets: the apply fires the
	// rescan, the stream must close.
	if err := chromedp.Run(ctx,
		chromedp.Click(`#to-plain`, chromedp.ByID),
		chromedp.WaitVisible(`#plain`, chromedp.ByID),
		chromedp.Poll(`window.__gofastr && window.__gofastr.sseStatus && window.__gofastr.sseStatus.connected === false`,
			nil, chromedp.WithPollingTimeout(10*time.Second), chromedp.WithPollingInterval(100*time.Millisecond)),
	); err != nil {
		t.Fatalf("chromedp (plain page): %v", err)
	}
	// The closed stream shows up as a finished network request
	// (loadingFinished or loadingFailed — a client-closed EventSource
	// reports ERR_ABORTED), and no second stream was opened.
	deadline := time.Now().Add(10 * time.Second)
	for log.finishedCount() < 1 && time.Now().Before(deadline) {
		if err := chromedp.Run(ctx, chromedp.Sleep(100*time.Millisecond)); err != nil {
			t.Fatalf("chromedp (waiting for stream close): %v", err)
		}
	}
	if log.finishedCount() < 1 {
		t.Fatalf("the SSE request never finished after leaving the last push target (finished=%d, opened=%v) — the stream stayed open", log.finishedCount(), log.opened)
	}
	if err := chromedp.Run(ctx, chromedp.Sleep(1500*time.Millisecond)); err != nil {
		t.Fatalf("chromedp (settle on plain page): %v", err)
	}
	if got := log.count(); got != 1 {
		t.Fatalf("plain page without push targets opened another stream: %d request(s): %v", got, log.opened)
	}

	// Back to the island page (a cached replay for the runtime): the
	// stream reopens, with the rolled-over session id.
	if err := chromedp.Run(ctx,
		chromedp.Click(`#to-home`, chromedp.ByID),
		chromedp.WaitVisible(`#home`, chromedp.ByID),
		chromedp.Poll(`window.__gofastr && window.__gofastr.sseStatus && window.__gofastr.sseStatus.connected === true`,
			nil, chromedp.WithPollingTimeout(10*time.Second), chromedp.WithPollingInterval(100*time.Millisecond)),
	); err != nil {
		t.Fatalf("chromedp (back to island page): %v", err)
	}
	if got := log.count(); got != 2 {
		t.Fatalf("returning to the island page opened %d stream(s) in total, want 2: %v", got, log.opened)
	}
	if !strings.Contains(log.opened[1], "session=sess-2") {
		t.Fatalf("reopened stream does not carry the current session id (want sess-2 from the X-Gofastr-Session rollover): %v", log.opened)
	}
}
