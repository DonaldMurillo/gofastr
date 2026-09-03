//go:build red

package setup

// RED TEST — open finding, 2026-09-03 adversarial pass round 4 (tests-only; no fix applied).
// Property: request bodies on an unauthenticated HTTP surface are size-capped
// by the app before parsing; the app's own bound must be the repo's form
// convention, not whatever the stdlib happens to default to.
// Surface: battery/setup/handler.go handleSubmit (:164) — req.ParseForm() with
// no MaxBytesReader, on POST /setup. The wizard is the bootstrap surface: with
// DisableToken (the documented trusted-network posture, already exercised by
// TestSetupProbeErrorFailsClosed) it is fully unauthenticated; with the token
// enabled it sits behind a single-use cookie an attacker who saw the URL can
// still hammer. The sibling auth battery caps the identical urlencoded form
// pattern at 1 MiB via MaxBytesReader and maps the overflow to 413
// (battery/auth/form_decode.go:71-78, json_limit.go:12 maxAuthBodyBytes),
// and crud/uihost/embed/relay all cap; no global body-limit middleware
// exists.
// Finding: handleSubmit sets no cap of its own. The only bound is the Go
// stdlib's internal 10 MiB urlencoded ceiling (go1.27 net/http "POST too
// large", surfaced as this handler's generic 400), so every body up to
// 10 MiB — 10x the auth sibling's cap — is buffered in full and processed
// on the unauthenticated bootstrap surface. Observed: a 4 MiB urlencoded
// POST to /setup runs the step and renders the wizard's next response
// (2xx/303) instead of a 413 refusal.
// Severity: LOW-MEDIUM — unauthenticated memory-amplification surface
// (bounded at 10 MiB/request by the stdlib) plus a parity gap against every
// other form surface in the repo; window is short on completed installs.
// Fix direction: wrap the body before parsing, exactly like the auth sibling:
// req.Body = http.MaxBytesReader(w, req.Body, cap) in handleSubmit (or in
// handleSetup's POST arm), map *http.MaxBytesError to 413; a cap in the
// auth-sibling's 1 MiB ballpark is the repo convention.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestSetupFormRedCapsBody(t *testing.T) {
	var runs atomic.Int32
	r := New(Config{
		DisableToken: true,
		Complete:     func(context.Context) (bool, error) { return false, nil },
		Steps: []Step{{
			Name: "probe",
			Run: func(context.Context, map[string]string) error {
				runs.Add(1)
				return nil
			},
		}},
	})
	h := r.Handler(func() {}, nil, nil)

	// 4 MiB urlencoded body — 4x the auth sibling's cap, comfortably under
	// the stdlib's 10 MiB floor so the only thing that could refuse it is an
	// app-level cap. No Origin/Sec-Fetch headers: a plain non-browser
	// client, the shape rejectCrossSiteForm deliberately lets through.
	body := "pad=" + strings.Repeat("a", 4<<20)
	req := httptest.NewRequest(http.MethodPost, "/setup", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge || runs.Load() != 0 {
		t.Errorf("SECURITY: [setup-body-cap] POST /setup with a 4 MiB urlencoded body returned %d with the step's Run invoked %d time(s) — "+
			"handleSubmit's bare req.ParseForm() (handler.go:164) sets no MaxBytesReader, so the unauthenticated bootstrap surface buffers and processes it "+
			"(the stdlib only refuses at its own 10 MiB ceiling, as a generic 400; the auth battery caps the same form shape at 1 MiB → 413, form_decode.go:71-78); "+
			"want 413 with the step never invoked",
			rec.Code, runs.Load())
	}
}
