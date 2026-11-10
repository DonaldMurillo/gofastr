package framework

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/middleware"
)

// What a retry gets after the default chain's request deadline answered
// the first attempt 504. The idempotency recorder starts at net/http's
// implicit 200 and used to cache any 2xx, so a handler that gave up when
// its context ended and returned without writing was cached as "200,
// empty body": the retry was served that with Idempotent-Replay: true for
// a write that never ran, and the handler was not run again for the TTL.
//
// The contract, both halves:
//   - the handler wrote nothing and its request is over: the claim is
//     released and nothing is cached, so the retry runs the handler;
//   - the handler wrote a 2xx, late: the work is done, so that response is
//     cached and the retry replays it instead of running the work twice.

// postTimedOut sends attempts with one Idempotency-Key through app's
// default chain: the first must be answered 504 by the timeout
// middleware; the retry is repeated while the abandoned first attempt
// still holds its claim (409) and returned once it is settled.
func postTimedOut(t *testing.T, app *App) (retry *http.Response, body string) {
	t.Helper()
	srv := httptest.NewServer(app.Router())
	t.Cleanup(srv.Close)
	post := func() (*http.Response, string) {
		req, err := http.NewRequest(http.MethodPost, srv.URL+"/charge", strings.NewReader(`{"amount":100}`))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(middleware.IdempotencyKeyHeader, "charge-1")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp, string(b)
	}
	if first, _ := post(); first.StatusCode != http.StatusGatewayTimeout {
		t.Fatalf("first attempt: status %d, want the timeout middleware's 504", first.StatusCode)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		retry, body = post()
		if retry.StatusCode != http.StatusConflict || time.Now().After(deadline) {
			return retry, body
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestIdempotencyDoesNotCacheUnwrittenResponseAfterTimeout(t *testing.T) {
	app := NewApp(
		WithConfig(AppConfig{RequestTimeout: 100 * time.Millisecond}),
		WithIdempotency(middleware.IdempotencyConfig{Principal: idemPrincipal}),
	)
	var runs, charged atomic.Int32
	app.Router().Post("/charge", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The first attempt meets a slow dependency and gives up when its
		// context ends, the way a handler does around db.ExecContext or an
		// outbound call. Later attempts find the dependency healthy.
		if runs.Add(1) == 1 {
			<-r.Context().Done()
			return // the request is over; there is no one to answer
		}
		charged.Add(1)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"charged":true}`))
	}))

	retry, body := postTimedOut(t, app)
	if retry.Header.Get("Idempotent-Replay") == "true" || retry.StatusCode != http.StatusCreated || charged.Load() != 1 {
		t.Fatalf("the retry of a request that timed out with 504 was answered %d %q with Idempotent-Replay=%q "+
			"(handler runs=%d, charges made=%d): want the handler run again and its 201",
			retry.StatusCode, body, retry.Header.Get("Idempotent-Replay"), runs.Load(), charged.Load())
	}
}

func TestIdempotencyReplaysLateWritten2xxAfterTimeout(t *testing.T) {
	app := NewApp(
		WithConfig(AppConfig{RequestTimeout: 100 * time.Millisecond}),
		WithIdempotency(middleware.IdempotencyConfig{Principal: idemPrincipal}),
	)
	var charged atomic.Int32
	app.Router().Post("/charge", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The charge goes through, but only after the deadline: the client
		// has its 504 by the time the handler answers.
		<-r.Context().Done()
		charged.Add(1)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"charged":true}`))
	}))

	retry, body := postTimedOut(t, app)
	if retry.StatusCode != http.StatusCreated || retry.Header.Get("Idempotent-Replay") != "true" || body != `{"charged":true}` {
		t.Fatalf("retry = %d %q, Idempotent-Replay=%q; want the first attempt's 201 replayed",
			retry.StatusCode, body, retry.Header.Get("Idempotent-Replay"))
	}
	if got := charged.Load(); got != 1 {
		t.Fatalf("the charge ran %d times, want 1: a retry after a 504 must not repeat work the handler finished", got)
	}
}
