//go:build red

package framework

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/middleware"
)

// Open finding from the 2026-11-10 bug hunt, kept as a red probe: it
// asserts the correct behaviour and fails until the fix lands. The
// claim has to be sized per request from the route budget the router
// stamps on the context, which changes the IdempotencyStore contract.
//
// the idempotency in-flight TTL is "sized and checked against
// the enforced request deadline" (CHANGELOG, idempotency.md), but both
// the sizing (NewApp) and the startup check (checkIdempotencyInFlightTTL)
// read only the app-wide budget. A route or group given a longer budget
// through the documented Router.SetRouteTimeout / SetTimeout (deploy.md)
// is enforced at THAT deadline, so its handler outlives the claim: Start
// accepts the pair without a word, and a retry of the same key runs the
// handler a second time while the first is still running — "exactly the
// double execution the key promised to prevent" (app.go).
func TestRedIdempotencyClaimOutlivedByRouteTimeout(t *testing.T) {
	app := NewApp(
		WithConfig(AppConfig{RequestTimeout: time.Second}),
		WithIdempotency(middleware.IdempotencyConfig{Principal: idemPrincipal}),
	)
	// The documented per-route override: one slow export keeps its own
	// budget, every other route stays at the app-wide one.
	const routeBudget = 30 * time.Second
	var runs atomic.Int32
	release := make(chan struct{})
	app.Router().Post("/exports", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runs.Add(1)
		select {
		case <-release:
		case <-r.Context().Done():
		}
		w.WriteHeader(http.StatusCreated)
	}))
	app.Router().SetRouteTimeout("POST", "/exports", routeBudget)

	ttl := app.idempotency.Store.(middleware.InFlightTTLStore).InFlightTTL()
	checkErr := app.checkIdempotencyInFlightTTL()
	if ttl > routeBudget || checkErr != nil {
		// Either fix is acceptable: a claim sized past the longest
		// route budget, or a startup refusal naming the pair.
		return
	}
	t.Logf("in-flight TTL %v, route budget %v, startup check error: %v", ttl, routeBudget, checkErr)

	srv := httptest.NewServer(app.Router())
	defer srv.Close()
	post := func() (int, error) {
		req, err := http.NewRequest(http.MethodPost, srv.URL+"/exports", strings.NewReader(`{"report":1}`))
		if err != nil {
			return 0, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(middleware.IdempotencyKeyHeader, "export-1")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return 0, err
		}
		resp.Body.Close()
		return resp.StatusCode, nil
	}

	first := make(chan int, 1)
	go func() {
		code, _ := post()
		first <- code
	}()
	// Wait for the first run to start, then until its claim has expired.
	// The handler is still inside its 30s route budget the whole time.
	deadline := time.Now().Add(5 * time.Second)
	for runs.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if runs.Load() != 1 {
		t.Fatalf("first request did not reach the handler (runs=%d)", runs.Load())
	}
	time.Sleep(ttl + 300*time.Millisecond)

	// The client's retry of the SAME key and body while the first run is
	// still in flight must get 409, not a second execution.
	second := make(chan int, 1)
	go func() {
		code, _ := post()
		second <- code
	}()
	var retryCode int
	select {
	case retryCode = <-second:
	case <-time.After(2 * time.Second):
		retryCode = -1 // it is parked inside the handler: a second run
	}
	got := runs.Load()
	close(release)
	<-first
	if retryCode == -1 {
		<-second
	}
	if got != 1 {
		t.Fatalf("handler ran %d times for one Idempotency-Key while the first run was still inside its %v route budget "+
			"(retry status %d, want 409 in-flight): the App sized the claim at %v from the app-wide RequestTimeout and "+
			"its startup check accepted it", got, routeBudget, retryCode, ttl)
	}
}
