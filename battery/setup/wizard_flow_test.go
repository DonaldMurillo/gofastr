package setup

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// servedLikeFramework wires r the way framework.App.Start does: the
// server serves whatever a handler cell holds, the cell starts on the
// setup surface, and swap REPLACES it with the real router. A no-op swap
// (what most older tests pass) hides every bug where the wizard swaps
// too early, because the next request still reaches the wizard.
//
// The real router answers "real app" on / and /orders and 404 on
// everything else, /setup included, as a real app does.
func servedLikeFramework(r *Runner) (http.Handler, *atomic.Int32) {
	realApp := http.NewServeMux()
	for _, p := range []string{"/{$}", "/orders"} {
		realApp.HandleFunc("GET "+p, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, "real app")
		})
	}
	var cell atomic.Pointer[http.Handler]
	var swaps atomic.Int32
	var once sync.Once
	setupH := r.Handler(func() {
		once.Do(func() {
			swaps.Add(1)
			var h http.Handler = realApp
			cell.Store(&h)
		})
	}, nil, nil)
	cell.Store(&setupH)
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		(*cell.Load()).ServeHTTP(w, req)
	}), &swaps
}

// fakeClock is a settable clock for the 503 path's re-check rate limit.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// GOFASTR_SETUP=force on a completed install keeps the wizard up until
// its steps have run: the first GET /setup used to fire the swap, so the
// operator's POST landed on the real router (404) and no step ran.
func TestForceKeepsWizardUntilStepsRun(t *testing.T) {
	t.Setenv("GOFASTR_SETUP", "force")
	var runs atomic.Int32
	r := New(Config{
		DisableToken: true,
		Complete:     func(context.Context) (bool, error) { return true, nil },
		Steps: []Step{{
			Name:   "rescue",
			Fields: []Field{{Name: "A", Label: i18n.Define("setup_test.a", "A")}},
			Run: func(context.Context, map[string]string) error {
				runs.Add(1)
				return nil
			},
		}},
	})
	h, swaps := servedLikeFramework(r)

	w := doGet(h, "/setup")
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "Setup Complete") {
		t.Fatalf("force GET /setup: code %d, want the step form, not the completion page", w.Code)
	}
	if swaps.Load() != 0 {
		t.Fatal("force mode swapped to the real app before any step ran")
	}

	w = doPost(h, "/setup", "A=x", nil)
	if runs.Load() != 1 {
		t.Fatalf("force POST /setup ran the step %d times (code %d), want 1", runs.Load(), w.Code)
	}
	if !strings.Contains(w.Body.String(), "Setup Complete") || swaps.Load() != 1 {
		t.Fatalf("after the last step: completion page %v, swaps %d; want both",
			strings.Contains(w.Body.String(), "Setup Complete"), swaps.Load())
	}
	if w = doGet(h, "/"); w.Body.String() != "real app" {
		t.Fatalf("after the wizard, / served %q, want the real app", w.Body.String())
	}
}

// The documented wiring is [adminStep, HealthStep(app)]: the admin step
// flips Complete, and the wizard used to call it done right there, so
// HealthStep never ran. Every declared step runs.
func TestWizardRunsStepsAfterComplete(t *testing.T) {
	var done atomic.Bool
	var healthRuns atomic.Int32
	r := New(Config{
		DisableToken: true,
		Complete:     func(context.Context) (bool, error) { return done.Load(), nil },
		Steps: []Step{
			{Name: "admin", Fields: []Field{{Name: "A", Label: i18n.Define("setup_test.a", "A")}},
				Run: func(context.Context, map[string]string) error { done.Store(true); return nil }},
			{Name: "health",
				Run: func(context.Context, map[string]string) error { healthRuns.Add(1); return nil }},
		},
	})
	h, swaps := servedLikeFramework(r)

	if w := doPost(h, "/setup", "A=x", nil); w.Code != http.StatusSeeOther {
		t.Fatalf("first step POST: code %d, want 303", w.Code)
	}
	if swaps.Load() != 0 {
		t.Fatal("swapped after the first of two steps")
	}

	w := doGet(h, "/setup")
	if strings.Contains(w.Body.String(), "Setup Complete") || !strings.Contains(w.Body.String(), "health") {
		t.Fatalf("GET /setup after the Complete-flipping step must render the next step (code %d)", w.Code)
	}
	if swaps.Load() != 0 {
		t.Fatal("swapped before the health step ran")
	}

	w = doPost(h, "/setup", "", nil)
	if healthRuns.Load() != 1 {
		t.Fatalf("health step ran %d times (code %d), want 1", healthRuns.Load(), w.Code)
	}
	if !strings.Contains(w.Body.String(), "Setup Complete") || swaps.Load() != 1 {
		t.Fatalf("after the last step: completion page %v, swaps %d; want both",
			strings.Contains(w.Body.String(), "Setup Complete"), swaps.Load())
	}
}

// The first step flips Complete while it is still running (AdminStep
// inserts the admin before returning). A request landing in that window
// sees "complete, no step finished here" and must not take it for setup
// done elsewhere: the swap would strand the operator's next POST.
func TestNoSwapWhileFirstStepRuns(t *testing.T) {
	var done atomic.Bool
	started, release := make(chan struct{}), make(chan struct{})
	r := New(Config{
		DisableToken: true,
		Complete:     func(context.Context) (bool, error) { return done.Load(), nil },
		Steps: []Step{
			{Name: "admin", Fields: []Field{{Name: "A"}},
				Run: func(context.Context, map[string]string) error {
					done.Store(true)
					close(started)
					<-release
					return nil
				}},
			{Name: "health", Run: func(context.Context, map[string]string) error { return nil }},
		},
	})
	h, swaps := servedLikeFramework(r)

	posted := make(chan int)
	go func() { posted <- doPost(h, "/setup", "A=x", nil).Code }()
	<-started
	w := doGet(h, "/setup")
	close(release)
	if code := <-posted; code != http.StatusSeeOther {
		t.Fatalf("first step POST: code %d, want 303", code)
	}
	if swaps.Load() != 0 || strings.Contains(w.Body.String(), "Setup Complete") {
		t.Fatalf("GET /setup while the first step ran: swaps %d, completion page %v; want neither",
			swaps.Load(), strings.Contains(w.Body.String(), "Setup Complete"))
	}
}

// Completed at first entry, with no step run in this process: the wizard
// is over (another replica, or a previous boot, finished it).
func TestWizardDoneWhenCompleteAtEntry(t *testing.T) {
	var runs atomic.Int32
	r := New(Config{
		DisableToken: true,
		Complete:     func(context.Context) (bool, error) { return true, nil },
		Steps: []Step{{Name: "s", Fields: []Field{{Name: "A"}},
			Run: func(context.Context, map[string]string) error { runs.Add(1); return nil }}},
	})
	h, swaps := servedLikeFramework(r)

	w := doGet(h, "/setup")
	if !strings.Contains(w.Body.String(), "Setup Complete") || swaps.Load() != 1 {
		t.Fatalf("GET /setup on a completed install: completion %v, swaps %d",
			strings.Contains(w.Body.String(), "Setup Complete"), swaps.Load())
	}
	if runs.Load() != 0 {
		t.Fatal("a step ran on a completed install without force")
	}
}

// A replica that did not run the wizard: the operator finished on
// replica A, and replica B answered 503 on every app path forever (and
// 403 on /setup, B never issued them a cookie). Any path now re-checks
// Complete, at most once per interval, and swaps.
func TestCompletedElsewhereSwapsOnAnyPath(t *testing.T) {
	build := func(t *testing.T) (*Runner, *atomic.Bool, *atomic.Int32, *fakeClock) {
		t.Helper()
		var done atomic.Bool
		var probes atomic.Int32
		r := New(Config{ // token enabled: B's operator holds no cookie
			Complete: func(context.Context) (bool, error) {
				probes.Add(1)
				return done.Load(), nil
			},
			Steps: []Step{{Name: "s", Fields: []Field{{Name: "A"}},
				Run: func(context.Context, map[string]string) error { return nil }}},
		})
		clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}
		r.now = clk.now
		return r, &done, &probes, clk
	}

	t.Run("app path", func(t *testing.T) {
		r, done, probes, clk := build(t)
		h, swaps := servedLikeFramework(r)

		if w := doGet(h, "/orders"); w.Code != http.StatusServiceUnavailable {
			t.Fatalf("before completion: code %d, want 503", w.Code)
		}
		done.Store(true) // the operator finished on replica A

		// Inside the interval: no probe, still 503. A 503 storm must not
		// turn into one Complete query per request.
		before := probes.Load()
		for range 20 {
			if w := doGet(h, "/orders"); w.Code != http.StatusServiceUnavailable {
				t.Fatalf("inside the re-check interval: code %d, want 503", w.Code)
			}
		}
		if got := probes.Load() - before; got != 0 {
			t.Fatalf("Complete probed %d times inside one interval, want 0", got)
		}

		clk.advance(time.Second)
		w := doGet(h, "/orders")
		if swaps.Load() != 1 {
			t.Fatalf("after the interval: swaps %d (code %d), want 1", swaps.Load(), w.Code)
		}
		if w.Code != http.StatusTemporaryRedirect || w.Header().Get("Location") != "/orders" {
			t.Fatalf("the healing request: code %d Location %q, want 307 to /orders",
				w.Code, w.Header().Get("Location"))
		}
		if w = doGet(h, "/orders"); w.Body.String() != "real app" {
			t.Fatalf("after the swap /orders served %q, want the real app", w.Body.String())
		}
	})

	// The healing redirect names this host's path. A request path of
	// //evil.example/x reaches the setup handler uncleaned (the setup
	// handler is the server's root, no ServeMux in front), and echoing it
	// as Location is a scheme-relative redirect off-site.
	t.Run("double-slash path", func(t *testing.T) {
		r, done, _, clk := build(t)
		h, _ := servedLikeFramework(r)
		done.Store(true)
		clk.advance(time.Second)
		w := doGet(h, "//evil.example/x?q=1")
		loc := w.Header().Get("Location")
		if w.Code != http.StatusTemporaryRedirect || loc != "/evil.example/x?q=1" {
			t.Fatalf("code %d Location %q, want 307 to /evil.example/x?q=1", w.Code, loc)
		}
	})

	t.Run("setup without cookie", func(t *testing.T) {
		r, done, _, _ := build(t)
		h, swaps := servedLikeFramework(r)
		done.Store(true)

		w := doGet(h, "/setup")
		if swaps.Load() != 1 || !strings.Contains(w.Body.String(), "Setup Complete") {
			t.Fatalf("cookieless /setup on a completed install: code %d swaps %d, want the completion page and a swap",
				w.Code, swaps.Load())
		}
	})

	t.Run("not complete", func(t *testing.T) {
		r, _, _, clk := build(t)
		h, swaps := servedLikeFramework(r)
		for range 3 {
			if w := doGet(h, "/setup"); w.Code != http.StatusForbidden {
				t.Fatalf("cookieless /setup while incomplete: code %d, want 403", w.Code)
			}
			if w := doGet(h, "/orders"); w.Code != http.StatusServiceUnavailable {
				t.Fatalf("app path while incomplete: code %d, want 503", w.Code)
			}
			clk.advance(time.Second)
		}
		if swaps.Load() != 0 {
			t.Fatal("swapped while Complete reports false")
		}
	})

	t.Run("force", func(t *testing.T) {
		t.Setenv("GOFASTR_SETUP", "force")
		r, done, _, _ := build(t)
		h, swaps := servedLikeFramework(r)
		done.Store(true)
		if w := doGet(h, "/orders"); w.Code != http.StatusServiceUnavailable {
			t.Fatalf("force mode app path: code %d, want 503 until the wizard runs", w.Code)
		}
		if swaps.Load() != 0 {
			t.Fatal("force mode swapped from the 503 path before the wizard ran")
		}
	})
}

// The probe interval holds across concurrent callers too: one probe per
// interval, however many 503s arrive at once.
func TestRecheckOnePerInterval(t *testing.T) {
	var probes atomic.Int32
	r := New(Config{
		Complete: func(context.Context) (bool, error) { probes.Add(1); return false, nil },
		Steps:    []Step{{Name: "s", Run: func(context.Context, map[string]string) error { return nil }}},
	})
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	r.now = clk.now
	h := r.Handler(func() {}, nil, nil)

	const callers = 64
	var wg sync.WaitGroup
	gate := make(chan struct{})
	for range callers {
		wg.Go(func() {
			req := httptest.NewRequest(http.MethodGet, "/x", nil)
			<-gate
			h.ServeHTTP(httptest.NewRecorder(), req)
		})
	}
	close(gate)
	wg.Wait()
	if got := probes.Load(); got != 1 {
		t.Fatalf("%d concurrent 503s probed Complete %d times, want 1", callers, got)
	}
}
