package webbotauth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A saturated lookup budget is our own backpressure, not a verdict on
// the request. Reporting it as an invalid signature logs a warning
// about a signature nobody looked at, and in require mode tells a
// correctly-signed agent to go fix credentials that are fine. The
// package contract calls this case unverified — not enough information
// to decide — and it is retryable.
func TestResolverBusy_IsUnverifiedAndRetryable(t *testing.T) {
	for i := 0; i < maxConcurrentLookups; i++ {
		dnsSem <- struct{}{}
	}
	t.Cleanup(func() {
		for i := 0; i < maxConcurrentLookups; i++ {
			<-dnsSem
		}
	})

	res := gateE21Verify(t, nil, time.Now())

	if res.Outcome == OutcomeInvalid {
		t.Errorf("outcome = invalid (%s); a busy resolver says nothing about the signature", res.Reason)
	}
	if res.Outcome != OutcomeUnverified {
		t.Fatalf("outcome = %s (%s), want unverified", res.Outcome, res.Reason)
	}
	if !res.Retryable {
		t.Error("Retryable = false; the caller should be told to come back")
	}
	if !strings.Contains(res.Reason, "resolver busy") {
		t.Errorf("reason = %q, want it to name the busy resolver", res.Reason)
	}
}

// Require mode answers 503 with Retry-After for that case, not 403: a
// 403 plus Accept-Signature tells a compliant bot its authentication is
// wrong, which sends it to debug a signature that was never examined.
func TestRequireMode_AnswersUnavailableWhenResolverIsBusy(t *testing.T) {
	for i := 0; i < maxConcurrentLookups; i++ {
		dnsSem <- struct{}{}
	}
	t.Cleanup(func() {
		for i := 0; i < maxConcurrentLookups; i++ {
			<-dnsSem
		}
	})

	h := New(true, nil).Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler ran for an unverified request in require mode")
	}))

	r := httptest.NewRequest(http.MethodGet, "/orders", nil)
	r.Header.Set("Signature-Input",
		`sig1=("@authority" "signature-agent";key="sig1");created=1;expires=99999999999;keyid="k";tag="web-bot-auth"`)
	r.Header.Set("Signature", `sig1=:AAAA:`)
	r.Header.Set("Signature-Agent", `sig1="https://agent.example"`)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	if rec.Code == http.StatusForbidden {
		t.Fatal("answered 403: that tells a correctly-signed agent its credentials are wrong")
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("503 carries no Retry-After")
	}
}
