package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// A status set before a signalling reconnect is still room.self.status
// after it. The server forgets a dropped socket's status and never
// echoes a status back to its sender, so the module puts the
// remembered document back on the new self itself.
func TestRTCReconnectKeepsSelfStatus(t *testing.T) {
	env := rtcSetup(t)

	rtcPollTrue(t, env.a, `!!(window.__room && window.__room.status.phase === 'hydrated')`, 10*time.Second)
	rtcFront(t, env.a, chromedp.Evaluate(`window.__room.setStatus({ muted: true });
      window.__gen0 = window.__room.status.generation;`, nil))
	if !env.room.kill("alpha") {
		t.Fatal("no alpha socket to kill")
	}
	rtcPollTrue(t, env.a, `!!(window.__room.status.generation >= window.__gen0 + 1 && window.__room.status.phase === 'hydrated')`, 20*time.Second)
	if got := rtcEval(t, env.a, `String(JSON.stringify(window.__room.self && window.__room.self.status))`); got != `{"muted":true}` {
		t.Fatalf("room.self.status after the reconnect = %s, want the document set before it", got)
	}
	// onSnapshot sees it too: the restore runs before the hook.
	if got := rtcEval(t, env.a, `String(JSON.stringify(window.__rtc.snapStatus))`); got != `{"muted":true}` {
		t.Fatalf("onSnapshot self.status after the reconnect = %s, want {\"muted\":true}", got)
	}
}

// One failed load of the ws module does not disable rtc for the rest
// of the page: the loader drops a failed load, and the next
// connectRoom fetches the module again and joins.
func TestRTCRetriesFailedWSModuleLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("browser E2E disabled in short mode")
	}
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	wsMod, ok := Module("ws")
	if !ok {
		t.Fatal("ws module not embedded")
	}
	rtcMod, ok := Module("rtc")
	if !ok {
		t.Fatal("rtc module not embedded")
	}
	room := newRTCRoom()
	t.Cleanup(room.stop)
	var wsFetches atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(js))
	})
	mux.HandleFunc("/__gofastr/runtime/rtc.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(rtcMod))
	})
	// The first fetch fails the way a transient network error does.
	mux.HandleFunc("/__gofastr/runtime/ws.js", func(w http.ResponseWriter, _ *http.Request) {
		if wsFetches.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(wsMod))
	})
	mux.HandleFunc("/ws", room.serveWS)
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><head></head><body>`+
			`<script src="/__gofastr/runtime.js"></script><script>
window.__phases = [];
window.__join = (tag) => __gofastr.connectRoom('ws://' + location.host + '/ws?peer=alpha', {
  channels: [],
  onPhase: (p) => { window.__phases.push(tag + ':' + p); },
});
__gofastr.loadModule('rtc').then(() => { window.__first = window.__join('first'); window.__booted = true; });
</script></body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	if err := chromedp.Run(ctx, chromedp.Navigate(srv.URL)); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	rtcPollTrue(t, ctx, `window.__phases.indexOf('first:closed:error') >= 0`, 10*time.Second)
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__second = window.__join('second'); true`, nil)); err != nil {
		t.Fatalf("second connectRoom: %v", err)
	}
	rtcPollTrue(t, ctx, `window.__phases.indexOf('second:hydrated') >= 0`, 15*time.Second)
	if n := wsFetches.Load(); n != 2 {
		t.Fatalf("ws module fetched %d times, want 2 (the failed load, then the retry)", n)
	}
}
