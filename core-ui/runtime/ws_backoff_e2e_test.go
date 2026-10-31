package runtime

import (
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// A server that accepts the handshake and closes with 4503 at once used
// to reset the backoff on every open: the client redialed every second
// forever. The delay must grow while no socket stays up.
func TestWSQuickCloseBacksOff(t *testing.T) {
	var mu sync.Mutex
	var dials []time.Time
	mux := http.NewServeMux()
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		dials = append(dials, time.Now())
		mu.Unlock()
		conn, err := wsHandshake(w, r)
		if err != nil {
			return
		}
		_, _ = conn.Write(wsCloseReason(4503, "try again"))
		_ = conn.Close()
	})
	script := wsConsoleTap + `
    __gofastr.loadModule('ws').then(() => {
      window.__ws = __gofastr.connectWebSocket('ws://' + location.host + '/ws', {});
      window.__ready = true;
    });
  `
	base := wsTestPage(t, mux, script)
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		chromedp.Poll(`window.__ready === true`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Poll(`window.__ws.status.generation >= 3`, nil, chromedp.WithPollingTimeout(15*time.Second), chromedp.WithPollingInterval(100*time.Millisecond)),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	// The client counts a generation when it starts the dial, before the
	// request reaches the server, so wait for the server's own count.
	deadline := time.Now().Add(10 * time.Second)
	for {
		mu.Lock()
		n := len(dials)
		mu.Unlock()
		if n >= 3 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("server saw %d dials, want 3", n)
		}
		time.Sleep(50 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	first, second := dials[1].Sub(dials[0]), dials[2].Sub(dials[1])
	if second < 1700*time.Millisecond {
		t.Fatalf("redial gaps %v then %v: the second must back off to about 2s", first, second)
	}
}
