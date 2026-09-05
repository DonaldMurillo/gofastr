package print

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
)

// Pins the battery-wide concurrency bound around RenderPDF, found by the
// 2026-09-04 red-probe round; fixed by a counting semaphore
// (Battery.renderSem, sized by Config.MaxConcurrentRenders, default 4)
// acquired in servePDF before Build, with requests past the cap answered
// 503 + Retry-After instead of spawning another renderer.
// Property: the per-document PDF route must bound how many renders a
// single caller (or a handful of callers) can drive concurrently — the
// shipped chromepdf backend allocates a headless-Chrome ExecAllocator per
// call, so an uncapped route behind nothing stronger than RequireAuth is
// a process-exhaustion primitive that takes the node down for every
// other tenant.
// Surfaces: battery/print/pdf.go:servePDF — the only production
// RenderPDF call site, shared by every declared document's /pdf route,
// so one battery-wide cap covers them all.

// gateRenderer counts simultaneous RenderPDF calls and holds each one
// until released, so concurrency is measured without sleeps.
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

// driveRenderCap fires n concurrent requests at a document's /pdf route
// through a gateRenderer, and asserts the decided behaviour: exactly
// wantCap renders run (held until release), every other request fails
// fast with 503 + Retry-After, and the held renders complete 200 after
// release.
func driveRenderCap(t *testing.T, cfg Config, wantCap, n int) {
	t.Helper()
	g := &gateRenderer{arrived: make(chan struct{}, n), release: make(chan struct{})}
	cfg.DefaultAccess = Public
	cfg.PDFRenderer = g

	b := New(cfg).Document(Document{
		Name: "doc", Path: "/doc",
		Build: func(*http.Request) (component.Component, error) {
			return stubDoc{html: "<p>x</p>"}, nil
		},
	})
	h := authed(mount(t, b))

	type result struct {
		code  int
		retry string
	}
	results := make(chan result, n)
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := get(t, h, "/print/doc/pdf")
			results <- result{code: rec.Code, retry: rec.Header().Get("Retry-After")}
		}()
	}

	// While the wantCap renders are held, the requests that did not get
	// a slot must fail fast with 503 + Retry-After.
	busy := 0
	deadline := time.After(10 * time.Second)
	for busy < n-wantCap {
		select {
		case res := <-results:
			if res.code != http.StatusServiceUnavailable {
				t.Errorf("request past the render cap: got %d, want 503", res.code)
			}
			if res.retry == "" {
				t.Errorf("503 render-busy response carries no Retry-After header")
			}
			busy++
		case <-deadline:
			t.Fatalf("only %d of %d overflow requests answered while %d renders held; "+
				"the route is not bounding render concurrency", busy, n-wantCap, wantCap)
		}
	}

	close(g.release)
	wg.Wait()
	close(results)
	for res := range results {
		if res.code != http.StatusOK {
			t.Errorf("render that held a slot: got %d, want 200", res.code)
		}
	}

	arrived := 0
	for {
		select {
		case <-g.arrived:
			arrived++
		default:
			g.mu.Lock()
			maxSeen := g.max
			g.mu.Unlock()
			if arrived != wantCap {
				t.Errorf("%d RenderPDF calls ran; want exactly the cap %d", arrived, wantCap)
			}
			if maxSeen > wantCap {
				t.Errorf("SECURITY: [print-render-fanout] peak simultaneous RenderPDF calls = %d, "+
					"cap is %d: servePDF must bound concurrent renders battery-wide", maxSeen, wantCap)
			}
			return
		}
	}
}

func TestPDFRenderConcurrencyBounded(t *testing.T) {
	t.Run("default cap", func(t *testing.T) {
		driveRenderCap(t, Config{}, defaultMaxConcurrentRenders, 8)
	})
	t.Run("configured cap", func(t *testing.T) {
		driveRenderCap(t, Config{MaxConcurrentRenders: 2}, 2, 8)
	})
}
