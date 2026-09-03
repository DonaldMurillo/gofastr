//go:build red

// RED TESTS — open findings, 2026-09-02 round-2 adversarial pass (tests-only; no fix applied).
// Property: request-derived log attributes must carry no C0 control bytes (0x00–0x1F) or DEL (0x7F) —
// the canonical percent-encoding scrubbers (scrubControlBytes / safeLogPath / safeLogMethod in
// core/middleware/logging.go) apply to every string that reaches an operator-visible log sink.
// Surfaces: battery/log/middleware.go:accessMiddleware, middleware.go:recoveryMiddleware,
// error_reporter.go:SlogErrorReporter.Report. Severity: production-facing (the production
// access-log / panic-reporting path; the default reporter writes to every configured sink).
// Finding: battery/log truncates every request-derived field (truncateString) but never scrubs
// control bytes. r.URL.Path is percent-DECODED, so "%0d%0a" in the raw request is a real CRLF
// in the `path` attr; the raw X-Forwarded-For flows into `forwarded_for` (and into `remote`
// when trustXFF is on); a panicking handler's value reaches the report/slog `panic` attr
// verbatim. core/middleware/recovery.go:53-55 scrubs the same values — battery/log does not.
// Fix direction: export the core/middleware scrubber (or lift it to a shared internal package)
// and percent-encode C0/DEL in accessMiddleware (path, forwarded_for, remote via remoteAddr),
// recoveryMiddleware (Error, Path), and SlogErrorReporter.Report (panic, path, method) —
// the reporter is a public seam (Plugin.Reporter) and must scrub what it is handed, not rely
// on the recovery middleware having done it.
package log

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

// assertNoCtrl fails when s carries any C0 control byte or DEL. Percent-encoded
// or stripped output passes; a raw or JSON-escaped control byte does not (the
// assertion runs on the DECODED attr value, matching scrubControlBytes semantics).
func assertNoCtrl(t *testing.T, attr, s string) {
	t.Helper()
	for i := range len(s) {
		if b := s[i]; b < 0x20 || b == 0x7f {
			t.Errorf("SECURITY: [log-ctrl] attr %q carries control byte 0x%02X at offset %d: %q — forged log lines / terminal-control payloads reach every operator sink; battery/log truncates but never scrubs (canonical: core/middleware scrubControlBytes)", attr, b, i, s)
			return
		}
	}
}

// decodeEntry decodes the single JSON log record in buf.
func decodeEntry(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var entry map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &entry); err != nil {
		t.Fatalf("decode log entry: %v (%q)", err, buf.String())
	}
	return entry
}

func strAttr(t *testing.T, entry map[string]any, key string) string {
	t.Helper()
	v, ok := entry[key].(string)
	if !ok {
		t.Fatalf("log entry missing string attr %q: %v", key, entry)
	}
	return v
}

// TestAccessLogRedScrubsPathBytes: the http.access record must not carry C0/DEL
// in its request-derived attrs. Two case shapes on one surface: percent-encoded
// CRLF in the path (decoded before it reaches the sink) and an XFF header
// carrying SOH (raw into forwarded_for; into remote when trusted).
func TestAccessLogRedScrubsPathBytes(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Shape 1: /x%0d%0ainjected — r.URL.Path is percent-decoded, so the
	// emitted `path` attr carries a real CRLF today.
	buf.Reset()
	h := accessMiddleware(logger, false)(ok)
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x%0d%0ainjected", nil))
	entry := decodeEntry(t, &buf)
	assertNoCtrl(t, "http.access.path", strAttr(t, entry, "path"))

	// Shape 2: X-Forwarded-For carrying SOH. Untrusted emits it raw in
	// forwarded_for; trusted also lets it override `remote`.
	buf.Reset()
	h2 := accessMiddleware(logger, true)(ok)
	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	req.Header.Set("X-Forwarded-For", "1.2.3.4\x01")
	h2.ServeHTTP(httptest.NewRecorder(), req)
	entry2 := decodeEntry(t, &buf)
	assertNoCtrl(t, "http.access.forwarded_for", strAttr(t, entry2, "forwarded_for"))
	assertNoCtrl(t, "http.access.remote", strAttr(t, entry2, "remote"))
}

// TestRecoveryLogRedScrubsPanic: the ErrorReport a recovered panic produces
// must not carry C0/DEL in its request-derived strings. A handler panicking
// with a terminal-title escape (ESC ]0; … BEL) plus a lone SOH, on a path with
// percent-encoded CRLF, hands both to the reporter verbatim today.
func TestRecoveryLogRedScrubsPanic(t *testing.T) {
	rep := &recordingReporter{}
	h := recoveryMiddleware(rep)(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		panic("boom \x1b]0;pwn\x07 \x01")
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/p%0d%0aq", nil))

	rep.mu.Lock()
	defer rep.mu.Unlock()
	if len(rep.reports) != 1 {
		t.Fatalf("got %d reports, want 1", len(rep.reports))
	}
	rp := rep.reports[0]
	assertNoCtrl(t, "ErrorReport.Error", rp.Error)
	assertNoCtrl(t, "ErrorReport.Path", rp.Path)
}

// TestErrorReporterRedScrubsAttrs: the DEFAULT reporter must scrub what it is
// handed. It is a public seam (Plugin.Reporter) — app code forwards reports
// without the recovery middleware in play — so a report whose Error/Path carry
// control bytes must not reach the logger verbatim.
func TestErrorReporterRedScrubsAttrs(t *testing.T) {
	var buf bytes.Buffer
	s := SlogErrorReporter{Logger: slog.New(slog.NewJSONHandler(&buf, nil))}
	s.Report(ErrorReport{
		Message: "http.panic",
		Error:   "err \x1b]0;pwn\x07 \x0b\x0c end",
		Method:  http.MethodGet,
		Path:    "/x\r\ninjected",
	})
	entry := decodeEntry(t, &buf)
	assertNoCtrl(t, "http.panic.panic", strAttr(t, entry, "panic"))
	assertNoCtrl(t, "http.panic.path", strAttr(t, entry, "path"))
}
