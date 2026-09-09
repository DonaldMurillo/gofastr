package log

// Property, found by the 2026-09-05 adversarial red-probe round 4
// (family F25, fixed the same round by widening this package's
// textsafe.ScrubControlBytes call sites and console needsQuoting with core/textsafe):
// request-derived fields of an http.access entry, a panic ErrorReport,
// and a console-rendered log line must not carry invisible or
// terminal-control characters in any encoding form — the C0+DEL scrub
// stopped at U+007F, so C1 controls (U+0080..U+009F, including 8-bit
// CSI/OSC and NEL) and the zero-width/bidi set (U+200B..U+200F, U+FEFF,
// U+2060, U+202A..U+202E, U+2066..U+2069) reached every sink raw, and
// the console sink's needsQuoting missed the same set so it rendered
// them BARE.
//
// Surfaces: battery/log/middleware.go (textsafe.ScrubControlBytes) via
// accessMiddleware (path from the percent-decoded URL, forwarded_for
// from the raw X-Forwarded-For header, remote from trusted
// XFF/X-Real-IP) and recoveryMiddleware (ErrorReport.Error/.Path),
// battery/log/console.go::formatValue+needsQuoting (attr values
// rendered bare into the terminal). Entries fan out to the file,
// webhook, console and MCP-ring sinks; 8-bit terminals execute
// 0x9B/0x9D as escapes, 0x85 as a newline, and RLO reorders the
// rendered line.

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

// hasInvisibleLogChar reports whether s carries a C1 control (U+0080..U+009F),
// a zero-width character (U+200B..U+200F, U+FEFF, U+2060), a bidi override
// (U+202A..U+202E) or a bidi isolate (U+2066..U+2069). Deliberately a
// local re-enumeration, not textsafe.ContainsUnsafe, so the test stays
// independent of the predicate it pins.
func hasInvisibleLogChar(s string) bool {
	for _, r := range s {
		switch {
		case r >= 0x80 && r <= 0x9F:
			return true
		case r >= 0x200B && r <= 0x200F:
			return true
		case r == 0xFEFF, r == 0x2060:
			return true
		case r >= 0x202A && r <= 0x202E:
			return true
		case r >= 0x2066 && r <= 0x2069:
			return true
		}
	}
	return false
}

// TestAccessInvisibleCharsScrubbed drives the four attack shapes through the
// access-log fields, the trusted-XFF remote field, the panic ErrorReport, and
// the console renderer — every request-derived sink in this package.
func TestAccessInvisibleCharsScrubbed(t *testing.T) {
	targets := []string{
		"/x%E2%80%AEadmin", // U+202E RIGHT-TO-LEFT OVERRIDE
		"/x%C2%9B31m",      // U+009B CSI, 8-bit escape introducer
		"/x%C2%85entry",    // U+0085 NEL, next-line control
		"/x%E2%80%8B",      // U+200B ZERO WIDTH SPACE
	}

	t.Run("access entry path and forwarded_for", func(t *testing.T) {
		for _, target := range targets {
			// Header values legally carry bytes >= 0x80, so the same
			// invisible characters ride the XFF header raw.
			xff := "junk "
			entry := driveAccess(t, http.MethodGet, target, map[string]string{
				"X-Forwarded-For": xff + "\u009bCSI\u202eRLO",
			})
			for _, field := range []string{"path", "forwarded_for"} {
				if s, _ := entry[field].(string); hasInvisibleLogChar(s) {
					t.Errorf("SECURITY: [log-invisible] http.access %s carries raw C1/zero-width/bidi characters for target %q: %q — entries fan out to the file, webhook, console and MCP-ring sinks where 8-bit terminals execute 0x9B/0x9D and NEL breaks lines", field, target, s)
				}
			}
		}
	})

	t.Run("trusted xff flows into remote", func(t *testing.T) {
		var buf bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&buf, nil))
		mw := accessMiddleware(logger, true)
		h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		req := httptest.NewRequest(http.MethodGet, "/p", nil)
		req.Header.Set("X-Forwarded-For", "1.2.3.4\u009b, 9.9.9.9")
		h.ServeHTTP(httptest.NewRecorder(), req)
		if hasInvisibleLogChar(buf.String()) {
			t.Errorf("SECURITY: [log-invisible] trusted-XFF `remote` field carries raw C1 characters: %q", buf.String())
		}
	})

	t.Run("recovery ErrorReport error and path", func(t *testing.T) {
		rep := &recordingReporter{}
		h := recoveryMiddleware(rep)(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
			panic("boom \u009b]0;pwn\u202e")
		}))
		for _, target := range targets {
			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, target, nil))
		}
		rep.mu.Lock()
		defer rep.mu.Unlock()
		if len(rep.reports) == 0 {
			t.Fatal("no reports recorded")
		}
		for i, rp := range rep.reports {
			if hasInvisibleLogChar(rp.Error) || hasInvisibleLogChar(rp.Path) {
				t.Errorf("SECURITY: [log-invisible] ErrorReport %d carries raw C1/zero-width/bidi characters (error=%q path=%q) — panic values and paths repaint every configured sink", i, rp.Error, rp.Path)
			}
		}
	})

	t.Run("console sink renders attr values bare", func(t *testing.T) {
		for _, esc := range []string{`\u009b`, `\u0085`, `\u202e`, `\u200b`} {
			entry := []byte(`{"time":"2026-01-01T00:00:00Z","level":"INFO","msg":"ok","path":"/x` + esc + `x"}`)
			var buf bytes.Buffer
			s := ConsoleSink(ConsoleOpts{Writer: &buf})
			if err := s.Write(entry); err != nil {
				t.Fatal(err)
			}
			if hasInvisibleLogChar(buf.String()) {
				t.Errorf("SECURITY: [log-invisible] console line renders a %s attr value bare: %q — needsQuoting must quote C1/bidi characters so they land in the terminal as JSON escapes, not raw runes", esc, buf.String())
			}
		}
	})
}
