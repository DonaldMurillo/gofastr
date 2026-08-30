package webbotauth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The legacy bare-string Signature-Agent header takes a different branch
// in agentRefFor, and that branch wrapped its error with %v — which
// renders identically and breaks the chain, so a saturated resolver came
// back as an invalid signature and require mode answered 403 to a
// correctly-signed agent. The dictionary form was unaffected, which is
// why the first pass at this missed it: the tests all used that form.
func TestResolverBusy_SurvivesTheLegacyAgentHeader(t *testing.T) {
	for _, tc := range []struct{ name, agent string }{
		{"dictionary form", `sig1="https://agent.example"`},
		{"legacy bare string", `"https://agent.example"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
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
				`sig1=("@authority" "signature-agent");created=1;expires=99999999999;keyid="k";tag="web-bot-auth"`)
			r.Header.Set("Signature", `sig1=:AAAA:`)
			r.Header.Set("Signature-Agent", tc.agent)

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, r)

			if rec.Code == http.StatusForbidden {
				t.Fatalf("answered 403 for %s: a busy resolver is not a verdict on the signature", tc.name)
			}
			if rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want 503", rec.Code)
			}
			if rec.Header().Get("Retry-After") == "" {
				t.Error("503 carries no Retry-After")
			}
			if body := rec.Body.String(); !strings.Contains(body, "resolver busy") {
				t.Errorf("503 body does not name the cause: %q", body)
			}
		})
	}
}
