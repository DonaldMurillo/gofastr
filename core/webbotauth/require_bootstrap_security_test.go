package webbotauth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Require mode refused every unverified request, the site's own key
// directory included. A remote verifier fetches that directory to check
// the signature on requests we send, so refusing it means nobody can
// reach the state require mode demands: the bootstrap deadlocks.
func TestRequireMode_LeavesTheKeyDirectoryReachable(t *testing.T) {
	reached := false
	h := New(true, nil).Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	}))

	t.Run("directory is served unsigned", func(t *testing.T) {
		reached = false
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, directoryWellKnown, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("unsigned %s = %d, want 200: a verifier cannot fetch our keys otherwise",
				directoryWellKnown, rec.Code)
		}
		if !reached {
			t.Error("handler never ran for the key directory")
		}
	})

	t.Run("every other path still requires a signature", func(t *testing.T) {
		reached = false
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/orders", nil))
		if rec.Code != http.StatusForbidden {
			t.Errorf("unsigned /orders = %d, want 403", rec.Code)
		}
		if reached {
			t.Error("handler ran for an unsigned request in require mode")
		}
	})
}

// Each Signature-Input member can cost a DNS check and a directory
// fetch, and the resolver coalesces only identical identifiers, so a
// sender naming distinct hosts buys sequential outbound work with one
// cheap request. The count is capped before any of that work starts.
func TestVerifyRequest_CapsSignatureCount(t *testing.T) {
	var members []string
	for i := 0; i < maxSignaturesPerRequest+1; i++ {
		members = append(members, "sig"+string(rune('a'+i))+`=("@authority");created=1;keyid="k";tag="web-bot-auth"`)
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Signature-Input", strings.Join(members, ", "))
	r.Header.Set("Signature", `siga=:AAAA:`)

	res := New(false, nil).VerifyRequest(r)
	if res.Outcome != OutcomeInvalid {
		t.Fatalf("outcome = %v, want invalid for %d members", res.Outcome, len(members))
	}
	if !strings.Contains(res.Reason, "limit") {
		t.Errorf("reason = %q, want it to name the limit", res.Reason)
	}

	// The cap must not refuse a normal request: one signature, or two
	// during a key rotation, has to keep working.
	r2 := httptest.NewRequest(http.MethodGet, "/", nil)
	r2.Header.Set("Signature-Input", strings.Join(members[:2], ", "))
	r2.Header.Set("Signature", `siga=:AAAA:`)
	if res2 := New(false, nil).VerifyRequest(r2); strings.Contains(res2.Reason, "limit") {
		t.Errorf("two signatures were refused by the cap: %q", res2.Reason)
	}
}
