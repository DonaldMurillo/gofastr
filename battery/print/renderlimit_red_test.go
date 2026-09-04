//go:build red

package print

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
)

// CONTRACT-QUESTION red: what bounds concurrent PDF renders? Every request to a /pdf route
// calls PDFRenderer.RenderPDF, and the shipped implementation
// (battery/print/chromepdf.RenderPDF, chromepdf.go:82+) allocates a headless-Chrome
// ExecAllocator per call — tens of megabytes and a process each. The battery applies NO
// concurrency cap and no queue: N concurrent authenticated requests spawn N concurrent
// Chrome processes. The maintainer must decide the default (a semaphore in servePDF or in
// chromepdf, backpressure with 503, or documented host-side limiting). No doc or sibling
// pin chooses today. If the answer is "host-side", this test is deleted and the battery
// doc says so.
//
// RED TEST — open finding, 2026-09-04 adversarial pass round 3 (tests-only; no fix applied).
// Family: F1 Resource exhaustion from request-borne input
// Property: the per-document PDF route must bound how many renders a single caller (or a
// handful of callers) can drive concurrently — an amplification primitive behind nothing
// stronger than RequireAuth.
// Surfaces: battery/print/pdf.go:servePDF (no cap around b.cfg.PDFRenderer.RenderPDF at
// pdf.go:76) for every declared Document; ReservedEmbedPrefixes (print.go:137-146)
// acknowledges the amplification concern for embedded grants but the direct route is
// uncapped.
// Finding: 8 concurrent GETs to /print/doc/pdf produce 8 simultaneous RenderPDF
// invocations (measured with a counting stub below); with the real chromepdf wiring that
// is 8 simultaneous headless Chrome processes per caller batch. Observed by running this
// test.
// Severity: medium — unauthenticated-degree availability risk on any app that mounts the
// PDF route behind plain RequireAuth; memory/process exhaustion takes the node down for
// every other tenant.
// Fix direction: cap concurrent RenderPDF calls per battery (a counting semaphore with
// backpressure/503), or document that hosts must throttle the route themselves.

// gateRenderer counts simultaneous RenderPDF calls and holds each one until every expected
// call has arrived (or the rendezvous times out), so concurrency is measured without sleeps.
type gateRenderer struct {
	arrived chan struct{}
	release chan struct{}

	mu  sync.Mutex
	cur int
	max int
}

func (g *gateRenderer) RenderPDF(context.Context, string, PageConfig, string) ([]byte, error) {
	g.mu.Lock()
	g.cur++
	if g.cur > g.max {
		g.max = g.cur
	}
	g.mu.Unlock()

	g.arrived <- struct{}{}
	<-g.release

	g.mu.Lock()
	g.cur--
	g.mu.Unlock()
	return []byte("%PDF-1.4 fake"), nil
}

func TestPDFRenderConcurrencyBounded(t *testing.T) {
	const n = 8
	g := &gateRenderer{arrived: make(chan struct{}, n), release: make(chan struct{})}

	b := New(Config{DefaultAccess: Public, PDFRenderer: g}).Document(Document{
		Name: "doc", Path: "/doc",
		Build: func(*http.Request) (component.Component, error) {
			return stubDoc{html: "<p>x</p>"}, nil
		},
	})
	h := authed(mount(t, b))

	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := get(t, h, "/print/doc/pdf")
			if rec.Code != http.StatusOK {
				t.Errorf("render returned %d", rec.Code)
			}
		}()
	}

	// Rendezvous: every request must reach RenderPDF (or give up after a
	// generous window, which itself means the route serializes and the
	// property already holds).
	for i := range n {
		select {
		case <-g.arrived:
		case <-time.After(10 * time.Second):
			// Fewer than n renders ever ran concurrently: bounded.
			close(g.release)
			wg.Wait()
			g.mu.Lock()
			maxSeen := g.max
			g.mu.Unlock()
			if maxSeen >= n {
				t.Fatalf("inconsistent: only %d arrivals but max concurrency %d", i+1, maxSeen)
			}
			return
		}
	}
	close(g.release)
	wg.Wait()

	g.mu.Lock()
	maxSeen := g.max
	g.mu.Unlock()
	if maxSeen >= n {
		t.Errorf("SECURITY: [print-render-fanout] %d concurrent requests drove %d simultaneous RenderPDF calls: servePDF (pdf.go:62-87) applies no concurrency bound before invoking the renderer, and the shipped chromepdf implementation allocates a headless-Chrome ExecAllocator per call (chromepdf.go:82+). RequireAuth alone turns this into a process-exhaustion primitive.", n, maxSeen)
	}
}
