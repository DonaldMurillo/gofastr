package uihost

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// The plaintext-remote WARN's host field is request-borne (the Host
// header). net/http's header reader passes every byte >= 0x80 through,
// so a forged Host can carry a bidi override (U+202E) or a C1 control
// (U+009B). slog's JSON handler leaves those runes raw in the encoded
// line, where they reorder or repaint the log tail, so
// the log sink needs the full textsafe scrub, not just C0/DEL.

// TestSessionWarnHostScrubbedBeyondC0 pins that.
func TestSessionWarnHostScrubbedBeyondC0(t *testing.T) {
	var buf bytes.Buffer
	oldDefault := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	defer slog.SetDefault(oldDefault)
	// The warn is once-per-process: reset the Once in place so this test
	// sees it fire, and leave a fresh one behind (a used Once cannot be
	// copied back; vet's copylocks refuses the assignment).
	plaintextRemoteWarnOnce = sync.Once{}
	defer func() { plaintextRemoteWarnOnce = sync.Once{} }()

	r := httptest.NewRequest(http.MethodPost, "/login", nil)
	// Non-loopback plaintext origin: hardened cookie name + Secure flag
	// over plain HTTP is the warn's trigger.
	r.Host = "203.0.113.7\u202eevil.example\u009b"
	setSessionCookie(httptest.NewRecorder(), r, "sid")

	out := buf.String()
	if !strings.Contains(out, "session cookie sent Secure") {
		t.Fatalf("setup: expected the plaintext-remote warn to fire, got %q", out)
	}
	if strings.Contains(out, "\u202e") || strings.Contains(out, "\u009b") {
		t.Errorf("SECURITY: [uihost] warn line carries a raw bidi/C1 rune from the forged Host: %q", out)
	}
}
