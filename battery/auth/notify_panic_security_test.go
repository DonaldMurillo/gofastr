package auth

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// Pins: a recovered panic value reaching the default slog sink is
// scrubbed of C0/DEL/C1/bidi (core/textsafe.Recovered) — the register
// duplicate-notice goroutine included. Found by the 2026-09-06/07
// adversarial round 5, phase 2 (family enumeration, tier T2).
// Surface: battery/auth/core.go::deliverRegisterDuplicateNotice
// :617-622 — slog.Warn("register duplicate-notice sender panicked",
// ..., "panic", fmt.Sprint(p)) with the raw recovered value, on the
// off-timed-path delivery goroutine.
// Finding: a host-supplied RegisterEmailSender that panics on the
// duplicate-register notice with a value carrying raw control bytes
// (\x1b OSC opener, U+202E bidi override, U+009B CSI) gets them logged
// verbatim by slog.Default(); those bytes paint forged lines into the
// operator's tail (terminal-injection). The recovery itself is the
// designed net (the recovercallback contract) — the scrub of the
// recovered value is the open gap.
// Fix direction: strip unsafe runes (the core/textsafe.StripUnsafe rule
// core/middleware's scrubControlBytes implements) before logging.
// Probe note: the payload embeds the control bytes RAW (panic value),
// not %q-escaped — %q pre-escapes them and would hide the surface.

// redNotifyScrubSink is a minimal slog.Handler storing record attributes
// verbatim (no JSON/text escaping), so a test can detect raw control
// bytes a real handler would have escaped away.
type redNotifyScrubSink struct {
	mu    sync.Mutex
	attrs map[string]string
}

func (h *redNotifyScrubSink) Enabled(context.Context, slog.Level) bool { return true }

func (h *redNotifyScrubSink) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.attrs == nil {
		h.attrs = map[string]string{}
	}
	r.Attrs(func(a slog.Attr) bool {
		h.attrs[a.Key] = a.Value.String()
		return true
	})
	return nil
}

func (h *redNotifyScrubSink) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *redNotifyScrubSink) WithGroup(string) slog.Handler      { return h }

func (h *redNotifyScrubSink) redGet(key string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.attrs[key]
}

const redNotifyPayload = "boom \x1b]0;pwned \u202e and \u009bCSI"

// redNotifyPanicSender is a RegisterEmailSender whose Send panics with
// the probe payload — the double a host sender built on hostile input
// would be.
type redNotifyPanicSender struct{}

func (redNotifyPanicSender) Send(context.Context, string, string) error { panic(redNotifyPayload) }

func TestAuthNotifyRedPanicScrubbed(t *testing.T) {
	prev := slog.Default()
	sink := &redNotifyScrubSink{}
	slog.SetDefault(slog.New(sink))
	t.Cleanup(func() { slog.SetDefault(prev) })

	store := newMemoryUserStore()
	mgr := New(AuthConfig{
		JWTSecret:           "test-secret",
		SessionTTL:          time.Hour,
		SessionCookie:       "session_id",
		UserStore:           store,
		DevMode:             true,
		RegisterEmailSender: redNotifyPanicSender{},
	})
	mgr.Use(NewCorePlugin())
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("setup broken: Init: %v", err)
	}
	r := mountRoutes(mgr)

	jar := &cookieJar{}
	register := func(stage string) *httptest.ResponseRecorder {
		rec := jar.do(r, http.MethodPost, "/auth/register",
			map[string]string{"email": "dup@example.com", "password": "supersecret1"}, "")
		if rec.Code != http.StatusAccepted {
			t.Fatalf("setup broken: %s register: %d %s", stage, rec.Code, rec.Body.String())
		}
		return rec
	}
	register("first")     // creates the holder
	register("duplicate") // taken branch → notice goroutine → sender panics

	// The notice is delivered off the timed path on its own goroutine:
	// bounded wait for the panic log to land.
	var got string
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if got = sink.redGet("panic"); got != "" {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if got == "" {
		t.Fatalf("setup broken: duplicate-notice sender panic never reached the slog sink (attrs=%v)", sink.attrs)
	}
	for _, bad := range []string{"\x1b", "\u202e", "\u009b"} {
		if strings.Contains(got, bad) {
			t.Errorf("SECURITY: [auth-notify-panic-scrub] recovered panic value reached slog "+
				"unscrubbed: raw %q present in panic attr %q. Attack: sender-derived control/bidi "+
				"bytes forge lines in the operator's log tail (terminal-injection); every pinned "+
				"recover path (RecoveryFn, Timeout, SlogErrorReporter) scrubs before logging.", bad, got)
		}
	}
	if !strings.Contains(got, "boom") || !strings.Contains(got, "pwned") {
		t.Errorf("SECURITY: [auth-notify-panic-scrub] positive control: visible panic text was "+
			"destroyed on the way to the log (got %q)", got)
	}
}
