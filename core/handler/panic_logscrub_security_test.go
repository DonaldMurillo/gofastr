package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/scrubtest"
)

// Pins: a recovered panic value embedding request-derived text reaches
// the default slog sink scrubbed of C0/DEL/C1/bidi (2026-09-06 adversarial
// pass, round 5).
// Property: a recovered panic value embedding request-derived text reaches
// the default slog sink scrubbed of C0/DEL/C1/bidi — the scrubControlBytes
// rule. RecoveryFn and Timeout's late-panic log (core/middleware) both
// scrub+truncate; HandlerAdapter's own recover truncates only.
// Surfaces: core/handler/handler.go HandlerAdapter recover path —
// truncateLog(fmt.Sprint(rec), maxPanicLogLen) with no scrub.
// Finding: a handler that panics with a value carrying raw request-derived
// control bytes (\r, NUL, DEL, U+202E bidi override, U+009B CSI) gets them
// logged verbatim by slog.Default(); those bytes paint forged lines into
// the operator's tail (terminal-injection).
// Fix direction: strip unsafe runes (core/textsafe.StripUnsafe, the same
// rule core/middleware's scrubControlBytes implements) BEFORE truncateLog.
// Probe note: the panic value embeds the control bytes RAW (fmt %s), not
// %q-escaped — %q pre-escapes them, which would hide the surface.

// The scrub probe itself (verbatim slog capture) is internal/scrubtest's
// Sink — the shared shape of middleware's captureHandler, which every
// red-scrub probe used to replicate per package.

func TestHandlerAdapterRedPanicLogScrub(t *testing.T) {
	sink := &scrubtest.Sink{}
	scrubtest.InstallDefault(t, sink)

	// Request-derived text carrying raw C0/DEL/C1/bidi bytes, embedded in
	// the panic value the way a handler echoing user input would build it.
	malicious := "x\r\n\u202E\u009bINJECT\x00\x7f"
	h := HandlerAdapter[struct{}, struct{}](func(_ context.Context, _ struct{}) (struct{}, error) {
		panic(fmt.Sprintf("bad input %s", malicious))
	})

	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("setup broken: recovered panic did not produce 500 (got %d)", rec.Code)
	}
	got := sink.Get("error")
	if got == "" {
		t.Fatalf("setup broken: panic value never reached the slog sink (attrs=%v)", sink.Attrs())
	}
	for _, bad := range []string{"\r", "\u202E", "\u009B", "\x00", "\x7f"} {
		if strings.Contains(got, bad) {
			t.Errorf("SECURITY: [handler-panic-log-scrub] recovered panic value reached slog "+
				"unscrubbed: raw %q present in error attr %q. Attack: request-derived control/bidi "+
				"bytes forge lines in the operator's log tail (terminal-injection); every other "+
				"recover path scrubs before logging.", bad, got)
		}
	}

	// Positive control: benign panic text survives the log path intact.
	h2 := HandlerAdapter[struct{}, struct{}](func(_ context.Context, _ struct{}) (struct{}, error) {
		panic("benign panic message")
	})
	rec2 := httptest.NewRecorder()
	h2(rec2, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec2.Code != http.StatusInternalServerError {
		t.Fatalf("setup broken: benign panic did not produce 500 (got %d)", rec2.Code)
	}
	if !strings.Contains(sink.Get("error"), "benign panic message") {
		t.Errorf("SECURITY: [handler-panic-log-scrub] positive control: benign panic text was "+
			"destroyed on the way to the log (got %q)", sink.Get("error"))
	}
}
