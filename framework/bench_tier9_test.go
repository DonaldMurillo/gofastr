package framework

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// ============================================================================
// Tier 9 — streams, islands, UI host
//
// The framework's value proposition isn't just CRUD over JSON — it's the
// SSR-with-hydration UI runtime plus the SSE-and-island plumbing. These
// benchmarks measure the parts of that surface that don't fit the
// per-operation shape of Tiers 1-8.
//
// Caveat: in-memory `httptest.ResponseRecorder` removes wire latency and
// flushing semantics. SSE numbers in particular should be treated as
// "lower-bound encoding cost", not RPS over a real network.
// ============================================================================

// ----------------------------------------------------------------------------
// 9.1 — Real-volume streaming list (bypasses parsePagination's limit cap)
// ----------------------------------------------------------------------------

// BenchmarkT9_StreamingListRealVolume calls ServeStreamingList directly so
// it can exercise the streaming surface at limits parsePagination would
// otherwise clamp to ≤100. Measures per-row encode/write cost and
// throughput for the 1k / 5k / 10k row case.
func BenchmarkT9_StreamingListRealVolume(b *testing.B) {
	forEachBenchDialect(b, func(b *testing.B, db *sql.DB, _ Dialect) {
		const N = 10000
		app := setupBlogDomain(b, db, N, 0)
		_ = app

		// We need a real CrudHandler for the entity to call ServeStreamingList.
		entity, err := app.Registry.Get("posts")
		if err != nil {
			b.Fatalf("registry: %v", err)
		}
		ch := NewCrudHandler(entity, db)
		ch.Registry = app.Registry

		for _, limit := range []int{1000, 5000, 10000} {
			limit := limit
			b.Run(fmt.Sprintf("rows=%d", limit), func(b *testing.B) {
				cols := ch.VisibleFields()
				ctx := context.Background()
				b.ResetTimer()
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					rec := httptest.NewRecorder()
					req := httptest.NewRequest(http.MethodGet, "/posts", nil)
					ch.ServeStreamingList(ctx, rec, req, cols, nil, nil, nil, limit)
					if rec.Code != http.StatusOK && rec.Code != 0 {
						b.Fatalf("status %d", rec.Code)
					}
					b.ReportMetric(float64(rec.Body.Len()), "response_bytes")
				}
			})
		}
	})
}

// BenchmarkT9_StreamingVsBuffered_RealVolume compares the streaming and
// buffered code paths at a fair workload by calling each handler method
// directly. Buffered is serveList (via List()); streaming is
// ServeStreamingList. Both bypass the parsePagination cap so the
// comparison is honest at large N.
func BenchmarkT9_StreamingVsBuffered_RealVolume(b *testing.B) {
	forEachBenchDialect(b, func(b *testing.B, db *sql.DB, _ Dialect) {
		const N = 5000
		app := setupBlogDomain(b, db, N, 0)
		entity, _ := app.Registry.Get("posts")
		ch := NewCrudHandler(entity, db)
		ch.Registry = app.Registry

		// For buffered we hit the public surface but cap at the legal max
		// (100) and run multiple iterations to total 5000 rows — this is the
		// shape a client paginating through would naturally produce.
		b.Run("buffered-paginated-5000", func(b *testing.B) {
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				totalBytes := 0
				for page := 1; page <= 50; page++ {
					req := httptest.NewRequest(http.MethodGet,
						fmt.Sprintf("/posts?page=%d&limit=100", page), nil)
					rec := httptest.NewRecorder()
					app.Router.ServeHTTP(rec, req)
					totalBytes += rec.Body.Len()
				}
				b.ReportMetric(float64(totalBytes), "response_bytes")
			}
		})

		// Streaming hits ServeStreamingList directly with the real 5000 limit.
		b.Run("streaming-single-5000", func(b *testing.B) {
			cols := ch.VisibleFields()
			ctx := context.Background()
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				rec := httptest.NewRecorder()
				req := httptest.NewRequest(http.MethodGet, "/posts", nil)
				ch.ServeStreamingList(ctx, rec, req, cols, nil, nil, nil, 5000)
				b.ReportMetric(float64(rec.Body.Len()), "response_bytes")
			}
		})
	})
}

// ----------------------------------------------------------------------------
// 9.2 — SSE EventStream throughput end-to-end
// ----------------------------------------------------------------------------

// BenchmarkT9_SSEEventStream subscribes to /posts/_events via the real
// HTTP handler and measures the rate at which emitted events make it
// through the wire format and out to a subscriber. The recorder consumes
// bytes; the emit loop drives.
//
// Reports:
//   - events_delivered    how many events reached the subscriber
//   - events_dropped      how many were dropped (32-buffer overflow)
//   - bytes_received      total bytes the subscriber saw
//   - delivery_ratio      delivered / emitted
func BenchmarkT9_SSEEventStream(b *testing.B) {
	forEachBenchDialect(b, func(b *testing.B, db *sql.DB, _ Dialect) {
		app := setupBlogDomain(b, db, 0, 0)
		entity, _ := app.Registry.Get("posts")
		ch := NewCrudHandler(entity, db)
		ch.Registry = app.Registry
		ch.Events = app.Events()

		// Each iteration: open a streaming subscriber, fire N events, count
		// what made it through. The handler reads from a 32-cap buffer and
		// silently drops on overflow.
		const eventsPerIter = 500

		b.ResetTimer()
		var totalDelivered, totalDropped, totalBytes int64
		for i := 0; i < b.N; i++ {
			ctx, cancel := context.WithCancel(context.Background())
			rec := newCountingSSERecorder()
			req := httptest.NewRequest(http.MethodGet, "/posts/_events", nil).WithContext(ctx)

			subWG := sync.WaitGroup{}
			subWG.Add(1)
			go func() {
				defer subWG.Done()
				ch.EventStream()(rec, req)
			}()

			// Tiny pause so the Subscribe call in the handler wins the race
			// against the first emit.
			time.Sleep(2 * time.Millisecond)

			for j := 0; j < eventsPerIter; j++ {
				ch.EmitEvent(context.Background(), EntityCreated,
					map[string]any{"id": fmt.Sprintf("p%d_%d", i, j), "title": "x"})
			}
			// Drain time so the handler's goroutine writes the buffered events.
			time.Sleep(20 * time.Millisecond)
			cancel()
			subWG.Wait()

			delivered := rec.eventsDelivered.Load()
			dropped := int64(eventsPerIter) - delivered
			if dropped < 0 {
				dropped = 0
			}
			totalDelivered += delivered
			totalDropped += dropped
			totalBytes += int64(rec.bytesWritten.Load())
		}
		b.StopTimer()

		if b.N > 0 {
			b.ReportMetric(float64(totalDelivered)/float64(b.N), "events_delivered")
			b.ReportMetric(float64(totalDropped)/float64(b.N), "events_dropped")
			b.ReportMetric(float64(totalBytes)/float64(b.N), "bytes_received")
			ratio := float64(totalDelivered) / float64(int64(b.N)*int64(eventsPerIter))
			b.ReportMetric(ratio, "delivery_ratio")
		}
	})
}

// countingSSERecorder is a stripped-down ResponseWriter that counts events
// and bytes without allocating a growing buffer per byte. Used by the SSE
// stream benchmark to avoid letting the recorder dominate the allocation
// profile.
type countingSSERecorder struct {
	header          http.Header
	statusCode      int
	statusWritten   bool
	bytesWritten    atomic.Int64
	eventsDelivered atomic.Int64
}

func newCountingSSERecorder() *countingSSERecorder {
	return &countingSSERecorder{header: make(http.Header)}
}

func (c *countingSSERecorder) Header() http.Header { return c.header }
func (c *countingSSERecorder) WriteHeader(code int) {
	c.statusCode = code
	c.statusWritten = true
}
func (c *countingSSERecorder) Write(p []byte) (int, error) {
	if !c.statusWritten {
		c.statusCode = http.StatusOK
		c.statusWritten = true
	}
	c.bytesWritten.Add(int64(len(p)))
	// One "event: ..." line per delivered event.
	for _, b := range p {
		if b == '\n' {
			// crude: every 'data:' starts an event frame; let's count by
			// frame separator "\n\n" instead — but a simpler heuristic is
			// "count occurrences of \"type\":\"entity.created\"". Just
			// count "data: " line starts to approximate.
		}
		_ = b
	}
	// Accurate count: search for "\nevent: " substrings (one per frame
	// the handler emits).
	c.countEvents(p)
	return len(p), nil
}
func (c *countingSSERecorder) Flush() {}

func (c *countingSSERecorder) countEvents(p []byte) {
	// The SSE writer emits "event: <type>\ndata: ...\n\n" per event.
	// Count the "event: " line headers.
	const marker = "event: "
	i := 0
	for i < len(p) {
		j := indexByte(p[i:], 'e')
		if j < 0 {
			break
		}
		if i+j+len(marker) > len(p) {
			break
		}
		if string(p[i+j:i+j+len(marker)]) == marker {
			c.eventsDelivered.Add(1)
			i = i + j + len(marker)
			continue
		}
		i = i + j + 1
	}
}

func indexByte(p []byte, c byte) int {
	for i, b := range p {
		if b == c {
			return i
		}
	}
	return -1
}

// ----------------------------------------------------------------------------
// 9.3 — Island RPC round-trip
// ----------------------------------------------------------------------------

// BenchmarkT9_IslandRPC measures the cost of one island RPC swap: an
// HTTP GET against an island endpoint that renders a small HTML fragment
// (the wire shape the runtime expects).
//
// Models the production pattern: click on a sort header → fetch
// /islands/<name>?state=X → server returns new island HTML → runtime
// swaps just the data-fui-signal wrapper.
func BenchmarkT9_IslandRPC(b *testing.B) {
	rtr := newBenchRouter()
	rtr.GetFunc("/islands/posts/state", func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("p")
		// A representative island response: a small table fragment of
		// ~10 rows. This is the wire shape an island handler returns.
		out := render.Tag("div",
			map[string]string{"data-fui-signal": "posts-rows", "data-fui-signal-mode": "html"},
			renderRows(page, 10)...,
		)
		render.RespondHTML(w, out)
	})

	for _, pages := range []int{1, 5, 25} {
		pages := pages
		b.Run(fmt.Sprintf("pages=%d", pages), func(b *testing.B) {
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				for p := 1; p <= pages; p++ {
					req := httptest.NewRequest(http.MethodGet,
						fmt.Sprintf("/islands/posts/state?p=%d", p), nil)
					rec := httptest.NewRecorder()
					rtr.ServeHTTP(rec, req)
					if rec.Code != http.StatusOK {
						b.Fatalf("status %d", rec.Code)
					}
				}
			}
		})
	}
}

// BenchmarkT9_IslandRPC_Concurrency drives the same handler in parallel
// to measure how the island path holds up under load. Mirrors what
// happens when many users sort/filter simultaneously.
func BenchmarkT9_IslandRPC_Concurrency(b *testing.B) {
	rtr := newBenchRouter()
	rtr.GetFunc("/islands/posts/state", func(w http.ResponseWriter, r *http.Request) {
		out := render.Tag("div",
			map[string]string{"data-fui-signal": "posts-rows", "data-fui-signal-mode": "html"},
			renderRows("1", 10)...,
		)
		render.RespondHTML(w, out)
	})

	for _, par := range []int{1, 8, 64} {
		par := par
		b.Run(fmt.Sprintf("parallelism=%d", par), func(b *testing.B) {
			rec := newLatencyRecorder(b.N + par*8)
			b.SetParallelism(par)
			b.ReportAllocs()
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				req := httptest.NewRequest(http.MethodGet,
					"/islands/posts/state?p=1", nil)
				for pb.Next() {
					start := time.Now()
					w := httptest.NewRecorder()
					rtr.ServeHTTP(w, req)
					rec.record(time.Since(start))
					if w.Code != http.StatusOK {
						b.Fatalf("status %d", w.Code)
					}
				}
			})
			b.StopTimer()
			rec.report(b)
		})
	}
}

// renderRows is a tiny helper that emits N table-row-shaped HTML
// fragments. Representative payload for an island swap.
func renderRows(label string, n int) []render.HTML {
	out := make([]render.HTML, n)
	for i := 0; i < n; i++ {
		out[i] = render.Tag("div", map[string]string{"class": "row"},
			render.Tag("span", map[string]string{"class": "title"},
				render.Text(fmt.Sprintf("Post %s.%d", label, i))),
			render.Tag("span", map[string]string{"class": "status"},
				render.Text("published")),
		)
	}
	return out
}

// newBenchRouter returns an App's router without the default middleware
// chain, suitable for micro-benchmark workloads that don't want
// Logging/Recovery overhead skewing the numbers.
func newBenchRouter() *appRouter {
	a := NewApp(WithoutDefaultMiddleware())
	return &appRouter{a.Router}
}

type appRouter struct {
	inner interface {
		GetFunc(string, http.HandlerFunc)
		ServeHTTP(http.ResponseWriter, *http.Request)
	}
}

func (a *appRouter) GetFunc(p string, f http.HandlerFunc)             { a.inner.GetFunc(p, f) }
func (a *appRouter) ServeHTTP(w http.ResponseWriter, r *http.Request) { a.inner.ServeHTTP(w, r) }

// ----------------------------------------------------------------------------
// 9.4 — UI host full page render through SSR
// ----------------------------------------------------------------------------

// BenchmarkT9_UIHostPageRender measures the cost of serving one full page
// through the UI host: route resolution, layout wrap, screen render,
// runtime script injection, write.
//
// Compare against the bare framework `BenchmarkT7_JSON_GoFastr` to see
// what the SSR + hydration shell adds.
func BenchmarkT9_UIHostPageRender(b *testing.B) {
	site := app.NewApp("bench")
	site.SetDefaultLayout(app.NewLayout("main"))
	site.Register("/", &benchHomeScreen{}, nil)
	site.Register("/about", &benchAboutScreen{}, nil)

	host := uihost.New(site)
	fwApp := NewApp(WithoutDefaultMiddleware())
	fwApp.Mount(host)

	for _, path := range []string{"/", "/about"} {
		path := path
		b.Run(path, func(b *testing.B) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				rec := httptest.NewRecorder()
				fwApp.Router.ServeHTTP(rec, req)
				if rec.Code != http.StatusOK {
					b.Fatalf("status %d: %s", rec.Code, rec.Body.String())
				}
				b.ReportMetric(float64(rec.Body.Len()), "response_bytes")
			}
		})
	}
}

// benchHomeScreen is the minimum-meaningful screen: a heading and one
// paragraph. Designed to measure framework overhead rather than page
// complexity.
type benchHomeScreen struct{}

func (s *benchHomeScreen) ScreenTitle() string        { return "Home" }
func (s *benchHomeScreen) ScreenDescription() string  { return "bench home" }
func (s *benchHomeScreen) ScreenType() app.ScreenType { return app.ScreenPage }
func (s *benchHomeScreen) Render() render.HTML {
	return render.Tag("main", nil,
		render.Tag("h1", nil, render.Text("Home")),
		render.Tag("p", nil, render.Text("Bench home page.")),
	)
}

type benchAboutScreen struct{}

func (s *benchAboutScreen) ScreenTitle() string        { return "About" }
func (s *benchAboutScreen) ScreenDescription() string  { return "bench about" }
func (s *benchAboutScreen) ScreenType() app.ScreenType { return app.ScreenPage }
func (s *benchAboutScreen) Render() render.HTML {
	rows := make([]render.HTML, 50)
	for i := 0; i < 50; i++ {
		rows[i] = render.Tag("li", nil, render.Text(fmt.Sprintf("Item %d", i)))
	}
	return render.Tag("main", nil,
		render.Tag("h1", nil, render.Text("About")),
		render.Tag("ul", nil, rows...),
	)
}

var (
	_ component.Component = (*benchHomeScreen)(nil)
	_ component.Component = (*benchAboutScreen)(nil)
)

// Just to keep encoding/json imported for shared helpers if used.
var _ = json.NewEncoder
