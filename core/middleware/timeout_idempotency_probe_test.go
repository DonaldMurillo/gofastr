package middleware

// Probes from the issue #136 audit of core/middleware. The Timeout(0)
// defect the audit found is fixed and pinned in timeout tests; what stays
// here are the boundary and race probes for Timeout and two refutations
// against the SQL idempotency store through the full default chain.

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Deterministic boundary semantics of Timeout: a handler that finishes far
// under budget must never 504; a handler that sleeps its full budget must
// (nearly) always 504. The 49ms margin dwarfs sleep overshoot and goroutine
// spawn cost, so this is full-suite-safe.
func TestTimeoutBoundaryIsDeterministic(t *testing.T) {
	const budget = 50 * time.Millisecond
	h := Timeout(budget)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(time.Millisecond)
		_, _ = w.Write([]byte("OK"))
	}))
	for range 50 {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("handler finished 49ms under budget, got status %d", w.Code)
		}
	}

	hFull := Timeout(budget)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(budget) // consumes the whole budget
		_, _ = w.Write([]byte("OK"))
	}))
	var got504 int
	for range 20 {
		w := httptest.NewRecorder()
		hFull.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
		if w.Code == http.StatusGatewayTimeout {
			got504++
		}
	}
	if got504 < 18 {
		t.Fatalf("full-budget handler only 504'd %d/20 times; deadline not enforced", got504)
	}
}

// Race probe for the done-vs-timer select in Timeout (timeout.go:367-395).
//
// FINDING T2: when the handler goroutine closes `done` before the deadline
// but the middleware goroutine is descheduled past it, the select finds BOTH
// channels ready and picks uniformly at random — a request whose handler
// completed under budget is answered 504 in that window. Genuinely
// load-dependent, so it is gated behind an env var to stay full-suite-safe;
// run it while the machine is busy:
//
//	GOFASTR_TIMEOUT_RACE_PROBE=1 go test ./core/middleware/ -run TestTimeoutDoneVsTimerRaceProbe -count=1 -v
//
// Executed evidence (audit, 2026-09-01, 6 CPU burners):
//
//	raw 504s=693/2000, proven misfires=2 (handler finished in 2.556ms /
//	2.581ms of a 3ms budget), unattributed=617 → misfire count is a lower
//	bound. Fix: in `case <-timer.C:`, non-blockingly re-check `done` before
//
// abandoning.
//
// A 504 only counts as a MISFIRE when THIS request's handler provably
// finished under the budget; each request carries a generation number so a
// late write from an abandoned (timed-out) handler goroutine of a PREVIOUS
// request can't contaminate the measurement (that artifact produced phantom
// microsecond "misfires" in an earlier version of this probe).
func TestTimeoutDoneVsTimerRaceProbe(t *testing.T) {
	if os.Getenv("GOFASTR_TIMEOUT_RACE_PROBE") == "" {
		t.Skip("set GOFASTR_TIMEOUT_RACE_PROBE=1 to run the load-dependent T2 race probe")
	}
	const budget = 3 * time.Millisecond
	const sleep = budget - 1*time.Millisecond

	var mu sync.Mutex
	var gen, reportGen uint64
	var reportElapsed time.Duration

	h := Timeout(budget)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		start := time.Now()
		g := atomic.LoadUint64(&gen)
		time.Sleep(sleep)
		_, _ = w.Write([]byte("OK"))
		mu.Lock()
		if g == atomic.LoadUint64(&gen) { // still the current request
			reportGen, reportElapsed = g, time.Since(start)
		}
		mu.Unlock()
	}))

	var misfires, raw504, unattributed int
	for range 2000 {
		w := httptest.NewRecorder()
		mu.Lock()
		atomic.AddUint64(&gen, 1)
		reportGen, reportElapsed = 0, 0
		g := atomic.LoadUint64(&gen)
		mu.Unlock()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
		if w.Code != http.StatusGatewayTimeout {
			continue
		}
		raw504++
		mu.Lock()
		elapsed, mine := reportElapsed, reportGen == g
		mu.Unlock()
		switch {
		case mine && elapsed > 0 && elapsed < budget:
			misfires++
			if misfires <= 3 {
				t.Logf("MISFIRE: handler finished in %v (< budget %v) but client got 504", elapsed, budget)
			}
		case !mine:
			// 504 whose handler never reported back before the next
			// request started (abandoned handler still sleeping).
			unattributed++
		}
	}
	t.Logf("budget=%v sleep=%v: raw 504s=%d/2000, proven misfires=%d, unattributed=%d",
		budget, sleep, raw504, misfires, unattributed)
	if misfires != 0 {
		t.Fatalf("handler provably finished under budget yet got 504 %d times: the done-vs-timer select race is real", misfires)
	}
}

// Refutation probe: through the FULL default-chain ordering
// (Idempotency outside, SecurityHeaders inside), a replayed response
// still carries the security headers, strips Set-Cookie, and never leaks
// across principals — against the SQL store, not just the memory store.
func TestSQLReplayKeepsSecurityHeaders(t *testing.T) {
	_, store := openSQLIdemStore(t)

	var calls atomic.Int32
	inner := SecurityHeaders(SecurityHeadersConfig{ContentSecurityPolicy: "default-src 'self'"})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "secret-token", Path: "/"})
			w.Header().Set("X-Custom", "v")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":1}`))
		}),
	)
	mw := Idempotency(IdempotencyConfig{
		Store:     store,
		Principal: func(r *http.Request) string { return r.Header.Get("X-User-ID") },
	})(inner)

	do := func(user string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/orders", io.NopCloser(strings.NewReader(`{"qty":1}`)))
		r.Header.Set("X-User-ID", user)
		r.Header.Set(IdempotencyKeyHeader, "order-1")
		r.Header.Set("Content-Type", "application/json")
		mw.ServeHTTP(w, r)
		return w
	}

	first := do("alice")
	if first.Code != http.StatusCreated {
		t.Fatalf("first request: %d", first.Code)
	}
	if first.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("first response lost CSP")
	}

	replay := do("alice")
	if calls.Load() != 1 {
		t.Fatalf("handler ran %d times, want 1", calls.Load())
	}
	if replay.Header().Get("Idempotent-Replay") != "true" {
		t.Fatal("second request not marked as replay")
	}
	if got := replay.Header().Get("Content-Security-Policy"); got != "default-src 'self'" {
		t.Fatalf("replayed response CSP = %q, want the SecurityHeaders value (security headers must survive replay)", got)
	}
	if replay.Header().Get("X-Custom") != "v" {
		t.Fatal("replayed response lost handler header X-Custom")
	}
	if replay.Header().Get("Set-Cookie") != "" {
		t.Fatal("SECURITY: replayed response re-issued Set-Cookie (session fixation / credential replay)")
	}
	if replay.Code != http.StatusCreated || replay.Body.String() != `{"id":1}` {
		t.Fatalf("replay body/status drifted: %d %q", replay.Code, replay.Body.String())
	}

	// Same raw key, different principal: must NOT replay alice's body.
	bob := do("bob")
	if calls.Load() != 2 {
		t.Fatalf("cross-principal replay: handler ran %d times, want 2", calls.Load())
	}
	if bob.Header().Get("Idempotent-Replay") == "true" {
		t.Fatal("SECURITY: bob was served alice's cached response")
	}
	if bob.Body.String() == `{"id":1}` && bob.Header().Get("X-Owner") == "alice" {
		t.Fatal("unreachable guard")
	}
}

// Refutation probe: N concurrent Begins on the same key through the SQL
// store — exactly one fresh claim, everyone else ErrInFlight; after Finish
// all replays agree. At-most-once holds under contention.
func TestSQLConcurrentClaimsOneWinner(t *testing.T) {
	_, s := openSQLIdemStore(t)
	ctx := context.Background()
	const n = 32

	var winners, inFlight atomic.Int32
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, ok, err := s.Begin(ctx, "race-key", "fp-same")
			switch {
			case err == nil && !ok:
				winners.Add(1)
			case errors.Is(err, ErrInFlight):
				inFlight.Add(1)
			default:
				t.Errorf("unexpected Begin result: ok=%v err=%v", ok, err)
			}
		}()
	}
	close(start)
	wg.Wait()

	if w := winners.Load(); w != 1 {
		t.Fatalf("at-most-once violated: %d concurrent callers got a fresh claim, want exactly 1", w)
	}
	if inFlight.Load() != n-1 {
		t.Fatalf("expected %d ErrInFlight, got %d", n-1, inFlight.Load())
	}

	if err := s.Finish(ctx, "race-key", "fp-same", &IdempotentResponse{
		Status: http.StatusOK,
		Header: http.Header{"X-Winner": []string{"1"}},
		Body:   []byte("done"),
	}); err != nil {
		t.Fatalf("finish: %v", err)
	}
	for range n {
		got, ok, err := s.Begin(ctx, "race-key", "fp-same")
		if err != nil || !ok || got == nil || string(got.Body) != "done" {
			t.Fatalf("post-finish replay: ok=%v err=%v resp=%+v", ok, err, got)
		}
	}
}
