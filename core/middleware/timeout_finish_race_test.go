package middleware

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Once the handler's response has been committed, a timer that fires a
// moment later MUST NOT write a 504 on top of it. The deadline timer and
// the handler-completion path race by construction (the timer is stopped
// only after the select returns), so expire() is the one place that can
// refuse — finish() has to be visible to it.
func TestExpireRefusesAfterFinish(t *testing.T) {
	rec := httptest.NewRecorder()
	tw := newTimeoutWriter(rec)
	tw.WriteHeader(http.StatusOK)
	_, _ = tw.Write([]byte("OK"))
	tw.finish()

	if tw.expire() {
		t.Fatal("expire() allowed a 504 after finish() committed the response — " +
			"a late timer would append 'Gateway Timeout' to an already-sent 200")
	}
}

// End-to-end: handlers completing right around the deadline must never
// produce a body carrying both the handler's output and a timeout error.
// Can only fail when the race is real; passes trivially when it is not.
func TestNoTimeoutWriteAfterHandlerDone(t *testing.T) {
	const deadline = 2 * time.Millisecond
	h := Timeout(deadline)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(deadline) // finish exactly around the deadline
		fmt.Fprint(w, "OK")
	}))
	srv := httptest.NewServer(h)
	defer srv.Close()

	for i := range 2000 {
		resp, err := http.Get(srv.URL)
		if err != nil {
			continue // connection-level flake is not what this test measures
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		got := string(raw)
		if strings.Contains(got, "OK") && strings.Contains(got, "Gateway Timeout") {
			t.Fatalf("iter %d: response carries both handler output and a late 504: %q (status %d)",
				i, got, resp.StatusCode)
		}
	}
}
