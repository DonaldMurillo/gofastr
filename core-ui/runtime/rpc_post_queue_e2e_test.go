package runtime

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// postQueueRig serves page under a /rpc that records every body. The
// first request is held until release closes (or the test ends); every later one answers 204 at once. With dropFirst the first
// request's connection is closed with no answer, so its fetch rejects
// (Chrome resends a request whose reused connection closed, so the
// resend is dropped too: every request is, until the test reads got).
type postQueueRig struct {
	mu      sync.Mutex
	bodies  []string
	release chan struct{}
	settled bool // dropFirst: stop dropping once the test has read got
}

func (g *postQueueRig) got() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.settled = true
	return append([]string(nil), g.bodies...)
}

func newPostQueueRig(t *testing.T, page string, dropFirst bool) (*httptest.Server, *postQueueRig) {
	t.Helper()
	g := &postQueueRig{release: make(chan struct{})}
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprint(w, MustRuntimeJS())
	})
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/rpc", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(io.LimitReader(r.Body, 1<<16))
		g.mu.Lock()
		g.bodies = append(g.bodies, string(b))
		first := len(g.bodies) == 1
		drop := dropFirst && !g.settled
		g.mu.Unlock()
		if drop {
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Error("response writer cannot hijack")
				return
			}
			conn, _, err := hj.Hijack()
			if err == nil {
				conn.Close()
			}
			return
		}
		if first {
			select {
			case <-g.release:
			case <-r.Context().Done():
			}
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><body>`+page+`<script src="/__gofastr/runtime.js"></script></body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	t.Cleanup(func() {
		select {
		case <-g.release:
		default:
			close(g.release)
		}
	})
	return srv, g
}

const postQueueForm = `<form id="f" data-cui-rpc="/rpc"><input id="t" name="title" value="A"><button type="submit">Save</button></form>`

// submitAs sets the title and submits the form.
func submitAs(v string) string {
	return `(() => { document.getElementById('t').value = ` + fmt.Sprintf("%q", v) + `; document.getElementById('f').requestSubmit(); })()`
}

// A POST form saved as A, edited to B and saved again while A is still
// out sends B once A answers: the newest value reaches the server. D-09
// dropped B, so A landed and the page went on showing A.
func TestFormPOSTResubmitChangedIsQueued(t *testing.T) {
	srv, g := newPostQueueRig(t, postQueueForm, false)
	ctx := chromedptest.Context(t)
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL),
		chromedp.Evaluate(submitAs("A"), nil),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(submitAs("B"), nil),
		chromedp.Evaluate(submitAs("C"), nil),
		chromedp.Sleep(300*time.Millisecond),
	); err != nil {
		t.Fatal(err)
	}
	if b := g.got(); len(b) != 1 {
		t.Fatalf("while A is out the form sent %q, want A alone", b)
	}
	var busy bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById('f').getAttribute('aria-busy') === 'true'`, &busy)); err != nil {
		t.Fatal(err)
	}
	if !busy {
		t.Fatal("a form with a POST out and a save queued is not aria-busy")
	}
	close(g.release)
	if err := chromedp.Run(ctx,
		chromedp.Poll(`!document.getElementById('f').hasAttribute('aria-busy')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Sleep(200*time.Millisecond),
	); err != nil {
		t.Fatal(err)
	}
	b := g.got()
	if len(b) != 2 || b[0] != `{"title":"A"}` || b[1] != `{"title":"C"}` {
		t.Fatalf("A then B then C sent %q, want A then the latest (C) once", b)
	}
}

// The double click: a re-submit carrying the payload already in flight is
// dropped, and one that returns to it cancels a queued edit (the newest
// value is the one already out).
func TestFormPOSTResubmitSamePayloadDropped(t *testing.T) {
	srv, g := newPostQueueRig(t, postQueueForm, false)
	ctx := chromedptest.Context(t)
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL),
		chromedp.Evaluate(submitAs("A"), nil),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(submitAs("A"), nil),
		chromedp.Evaluate(submitAs("B"), nil),
		chromedp.Evaluate(submitAs("A"), nil),
		chromedp.Sleep(300*time.Millisecond),
	); err != nil {
		t.Fatal(err)
	}
	close(g.release)
	if err := chromedp.Run(ctx,
		chromedp.Poll(`!document.getElementById('f').hasAttribute('aria-busy')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Sleep(300*time.Millisecond),
	); err != nil {
		t.Fatal(err)
	}
	if b := g.got(); len(b) != 1 || b[0] != `{"title":"A"}` {
		t.Fatalf("A, A, B, A while A was out sent %q, want A once", b)
	}
}

// A hung POST unlocks the form after the runtime's 30 s request window:
// the test page scales that one delay to 300 ms. A queued save goes out
// when the window closes; before it, a re-submit stays held.
func TestFormPOSTHungRequestUnlocks(t *testing.T) {
	scale := `<script>(() => { const st = window.setTimeout; window.setTimeout = (f, ms, ...a) => st(f, ms === 30000 ? 600 : ms, ...a); })()</script>`
	srv, g := newPostQueueRig(t, scale+postQueueForm, false)
	ctx := chromedptest.Context(t)
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL),
		chromedp.Evaluate(submitAs("A"), nil),
		chromedp.Sleep(150*time.Millisecond),
		chromedp.Evaluate(submitAs("B"), nil),
		chromedp.Sleep(150*time.Millisecond),
	); err != nil {
		t.Fatal(err)
	}
	if b := g.got(); len(b) != 1 {
		t.Fatalf("before the window closed the form sent %q, want A alone", b)
	}
	if err := chromedp.Run(ctx, chromedp.Sleep(1200*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if b := g.got(); len(b) != 2 || b[1] != `{"title":"B"}` {
		t.Fatalf("after A hung past the window the form sent %q, want A then B", b)
	}
}

// A POST whose fetch rejects (the connection dropped) releases the form:
// the next submit of the same payload goes out.
func TestFormPOSTFailedFetchUnlocks(t *testing.T) {
	srv, g := newPostQueueRig(t, postQueueForm, true)
	ctx := chromedptest.Context(t)
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL),
		chromedp.Evaluate(submitAs("A"), nil),
		chromedp.Poll(`!document.getElementById('f').hasAttribute('aria-busy')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
	); err != nil {
		t.Fatal(err)
	}
	before := len(g.got())
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(submitAs("A"), nil),
		chromedp.Sleep(300*time.Millisecond),
	); err != nil {
		t.Fatal(err)
	}
	if b := g.got(); len(b) != before+1 {
		t.Fatalf("a submit after a failed fetch sent %d requests, want 1", len(b)-before)
	}
}
