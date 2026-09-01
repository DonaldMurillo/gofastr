package middleware

// Two ways Timeout could 504 a request that did not time out.
//
// Both were found auditing #136's "never-looked" list. Neither is reachable
// through the framework's own wiring — app.go installs the middleware only
// when RequestTimeout > 0, and the deadline race needs the middleware
// goroutine descheduled across the boundary — which is exactly why the
// existing suite was green over both.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"
)

// A zero duration means "no deadline" in net/http, and this file's own
// route-budget path already reads it that way ("a negative Budget skips the
// deadline entirely"). The constructor did not: time.NewTimer(0) fires
// before the handler can run, so EVERY request 504d.
//
// The trap is a config field left unset — `Timeout(cfg.RequestTimeout)`
// then turns the surface off rather than leaving it untimed, and it fails
// closed on every request rather than loudly at startup.
func TestTimeoutZeroOrNegativeMeansNoDeadline(t *testing.T) {
	for _, d := range []time.Duration{0, -time.Second} {
		t.Run(d.String(), func(t *testing.T) {
			var served int
			h := Timeout(d)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				served++
				// Long enough that any live deadline of d would fire.
				time.Sleep(20 * time.Millisecond)
				w.WriteHeader(http.StatusOK)
			}))
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

			if rec.Code != http.StatusOK {
				t.Errorf("Timeout(%v) returned %d, want 200: a non-positive duration means no deadline, as it does for a route budget and in net/http", d, rec.Code)
			}
			if served != 1 {
				t.Errorf("handler ran %d times, want 1", served)
			}
		})
	}

	// The route-override path, whose behaviour the constructor now matches.
	// If these ever disagree again, the same value means two things in one
	// file, which is how this got missed.
	t.Run("route budget agrees", func(t *testing.T) {
		h := Timeout(time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req = req.WithContext(WithRouteTimeout(req.Context(), RouteTimeout{Budget: 0}))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("RouteTimeout{Budget:0} returned %d, want 200", rec.Code)
		}
	})

	// The guard must not disarm a real deadline.
	t.Run("a positive duration still times out", func(t *testing.T) {
		h := Timeout(10 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(2 * time.Second)
			w.WriteHeader(http.StatusOK)
		}))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		if rec.Code != http.StatusGatewayTimeout {
			t.Errorf("a hung handler under a 10ms budget returned %d, want 504", rec.Code)
		}
	})
}

// The deadline branch re-checks done before abandoning, because a select
// whose cases are both ready picks between them at random. When the handler
// RETURNS just inside the budget but the middleware goroutine is descheduled
// past the deadline, that coin flip discards a finished response.
//
// This is a demonstration, not a regression gate, and the fix ships WITHOUT
// a test that catches its removal. Both halves of that are worth stating.
//
// No deterministic version exists. A select picks randomly only when both
// channels are ready at the moment it is *evaluated*; enter it earlier and
// it blocks and wakes on the first one. Producing the overlap needs the
// middleware goroutine descheduled across the deadline, which nothing
// outside the runtime can arrange.
//
// And the occurrence is unconfirmed HERE. The hazard is certain — the Go
// spec makes the ready-vs-ready choice uniform, so the branch is reachable
// by construction — but with the re-check removed this probe found
// 0 misfires in 4000 iterations under 10 CPU burners (~50 genuine overruns
// per 2000, so the deadline was firing plenty). An audit worker reported
// ~2 per 2000 on the same shape; that result is not reproduced here.
// The one-line tie-break is kept because it cannot make anything worse and
// the branch is reachable, not because a failure was observed.
//
// If you are reading this while chasing a spurious 504: run
//
//	GOFASTR_TIMEOUT_RACE_PROBE=1 go test ./core/middleware/ -run DeadlineRace
//
// under load, and note the instrument's one trap — the 504 path returns
// while the handler is still running, so reading its elapsed time straight
// after ServeHTTP reads zero for exactly the requests under test. Counting
// those as overruns makes the probe blind to what it is looking for. That
// is what the `finished` channel below is for; the first draft omitted it
// and confidently reported 0 in 4000.
func TestTimeoutDeadlineRaceProbe(t *testing.T) {
	if os.Getenv("GOFASTR_TIMEOUT_RACE_PROBE") == "" {
		t.Skip("load-dependent probe; set GOFASTR_TIMEOUT_RACE_PROBE=1")
	}
	const (
		budget = 3 * time.Millisecond
		work   = 2 * time.Millisecond
		iters  = 2000
	)
	var misfires, overruns int
	for i := 0; i < iters; i++ {
		var mu sync.Mutex
		var elapsed time.Duration
		// The 504 path returns while the handler goroutine is still
		// running, so reading elapsed straight after ServeHTTP reads zero
		// for exactly the requests under test — and classifying those as
		// overruns makes the probe blind to the thing it is looking for.
		// (First draft did that and reported 0 misfires in 4000.)
		finished := make(chan struct{})

		h := Timeout(budget)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			time.Sleep(work)
			w.WriteHeader(http.StatusOK)
			mu.Lock()
			elapsed = time.Since(start)
			mu.Unlock()
			close(finished)
		}))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

		select {
		case <-finished:
		case <-time.After(time.Second):
			t.Fatalf("iteration %d: handler never finished", i)
		}
		mu.Lock()
		e := elapsed
		mu.Unlock()
		switch {
		case rec.Code == http.StatusOK:
		case e >= budget:
			overruns++ // handler genuinely missed its budget
		default:
			misfires++
			t.Errorf("handler returned in %v, inside the %v budget, and the client got 504", e, budget)
		}
	}
	t.Logf("iterations=%d proven misfires=%d genuine overruns=%d", iters, misfires, overruns)
}

// The tie-break itself, at the seam.
//
// The RACE has no deterministic test (see the probe above). The DECISION
// does, and that is the part that can regress: if handlerAlreadyFinished
// stops reporting a closed done, the deadline branch discards finished
// responses again. Removing the `case <-done` arm fails this.
// The tie-break must deliver only for a handler that finished INSIDE its
// budget. A closed done alone would hand a 200 to a handler that overran by
// any amount whenever this goroutine was descheduled past its finish — the
// deadline would then bound nothing precisely when the scheduler is slow,
// which is when it matters. Reviewed onto the PR that introduced the
// tie-break; the first version checked only the channel.
func TestHandlerBeatTheDeadlineComparesFinishTime(t *testing.T) {
	deadline := time.Now()

	t.Run("finished inside the budget is delivered", func(t *testing.T) {
		done := make(chan struct{})
		close(done)
		fin := deadline.Add(-10 * time.Millisecond)
		if !handlerBeatTheDeadline(done, &fin, deadline) {
			t.Error("a handler that returned before the deadline must be delivered, not 504'd")
		}
	})

	t.Run("finished after the deadline is not", func(t *testing.T) {
		done := make(chan struct{})
		close(done)
		fin := deadline.Add(10 * time.Millisecond)
		if handlerBeatTheDeadline(done, &fin, deadline) {
			t.Error("a handler that overran its budget must still 504; delivering it makes the deadline bound nothing whenever this goroutine is descheduled")
		}
	})

	t.Run("exactly on the deadline is delivered", func(t *testing.T) {
		done := make(chan struct{})
		close(done)
		fin := deadline
		if !handlerBeatTheDeadline(done, &fin, deadline) {
			t.Error("finishing exactly at the deadline is not overrunning it")
		}
	})

	t.Run("still running is never delivered", func(t *testing.T) {
		var fin time.Time
		if handlerBeatTheDeadline(make(chan struct{}), &fin, deadline) {
			t.Error("an open done reported finished; the deadline would never fire")
		}
	})
}

func TestHandlerAlreadyFinishedReportsAClosedDone(t *testing.T) {
	closed := make(chan struct{})
	close(closed)
	if !handlerAlreadyFinished(closed) {
		t.Error("a closed done must report the handler finished; the deadline branch would 504 a completed response")
	}

	open := make(chan struct{})
	if handlerAlreadyFinished(open) {
		t.Error("an open done must not report the handler finished; the deadline would never fire")
	}

	// Must not block on an open channel — the deadline path has to make
	// progress whether or not the handler is done.
	returned := make(chan bool, 1)
	go func() { returned <- handlerAlreadyFinished(make(chan struct{})) }()
	select {
	case got := <-returned:
		if got {
			t.Error("an open done reported finished")
		}
	case <-time.After(time.Second):
		t.Error("handlerAlreadyFinished blocked on an open done")
	}
}

// The deadline still wins when the handler really did overrun, so the fix
// above is a tie-break and not a disarm.
func TestTimeoutStillAbandonsAnOverrunHandler(t *testing.T) {
	released := make(chan struct{})
	h := Timeout(10 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-released
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		close(released)
		t.Fatal("the middleware never returned for a hung handler")
	}
	if rec.Code != http.StatusGatewayTimeout {
		t.Errorf("hung handler returned %d, want 504", rec.Code)
	}
	close(released)
	_ = context.Canceled
}
