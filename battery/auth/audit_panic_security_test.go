package auth

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// Pins: a recovered panic value reaching the default slog sink is
// scrubbed of C0/DEL/C1/bidi (core/textsafe.Recovered), the
// scrubControlBytes rule pinned for RecoveryFn + Timeout's late-panic
// log (core/middleware) and battery/log's SlogErrorReporter. Found by
// the 2026-09-06/07 adversarial round 5, phase 2 (family enumeration,
// tier T2). Sibling arm: the magic-link reaper's recover log.
// Surface: battery/auth/audit.go::dispatch (emitSecurity) :101-107 —
// slog.Warn("auth: audit sink panic recovered; event lost", "kind",
// ev.Kind, "panic", r) with the raw recovered value.
// Finding: a host-supplied AuditSink that panics with a value carrying
// raw control bytes (\x1b OSC opener, U+202E bidi override, U+009B CSI)
// gets them logged verbatim by slog.Default(); those bytes paint forged
// lines into the operator's tail (terminal-injection) — and this is the
// security-audit funnel, exactly the trail an operator reads during an
// incident. The recovery itself is pinned (TestAudit_PanickingSink-
// Recovered) — the scrub of the recovered value is the open gap.
// Fix direction: strip unsafe runes (the core/textsafe.StripUnsafe rule
// core/middleware's scrubControlBytes implements) before logging.
// Probe note: the payload embeds the control bytes RAW (panic value),
// not %q-escaped — %q pre-escapes them and would hide the surface.

// redAuditScrubSink is a minimal slog.Handler storing record attributes
// verbatim (no JSON/text escaping), so a test can detect raw control
// bytes a real handler would have escaped away.
type redAuditScrubSink struct {
	mu    sync.Mutex
	attrs map[string]string
}

func (h *redAuditScrubSink) Enabled(context.Context, slog.Level) bool { return true }

func (h *redAuditScrubSink) Handle(_ context.Context, r slog.Record) error {
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

func (h *redAuditScrubSink) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *redAuditScrubSink) WithGroup(string) slog.Handler      { return h }

func (h *redAuditScrubSink) redGet(key string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.attrs[key]
}

// redAuditPanicSink is an AuditSink whose SecurityEvent panics with the
// probe payload (the same double a host sink built on hostile input
// would be).
type redAuditPanicSink struct{}

const redAuditPayload = "boom \x1b]0;pwned \u202e and \u009bCSI"

func (redAuditPanicSink) SecurityEvent(context.Context, SecurityEvent) { panic(redAuditPayload) }

func TestAuthAuditRedPanicScrubbed(t *testing.T) {
	prev := slog.Default()
	sink := &redAuditScrubSink{}
	slog.SetDefault(slog.New(sink))
	t.Cleanup(func() { slog.SetDefault(prev) })

	mgr := New(AuthConfig{
		JWTSecret:     "test-secret",
		SessionTTL:    time.Hour,
		SessionCookie: "session_id",
		UserStore:     newUserStoreWithPassword(),
		DevMode:       true,
		AuditSink:     redAuditPanicSink{},
	})
	mgr.Use(NewCorePlugin())
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("setup broken: Init: %v", err)
	}
	mgr.emitSecurity(context.Background(), SecurityEvent{Kind: "login.succeeded", UserID: "u-red"})

	got := sink.redGet("panic")
	if got == "" {
		t.Fatalf("setup broken: audit sink panic never reached the slog sink (attrs=%v)", sink.attrs)
	}
	for _, bad := range []string{"\x1b", "\u202e", "\u009b"} {
		if strings.Contains(got, bad) {
			t.Errorf("SECURITY: [auth-audit-panic-scrub] recovered panic value reached slog "+
				"unscrubbed: raw %q present in panic attr %q. Attack: sink-derived control/bidi "+
				"bytes forge lines in the operator's log tail (terminal-injection) — in the security-"+
				"audit funnel an operator reads during an incident; every pinned recover path "+
				"(RecoveryFn, Timeout, SlogErrorReporter) scrubs before logging.", bad, got)
		}
	}
	if !strings.Contains(got, "boom") || !strings.Contains(got, "pwned") {
		t.Errorf("SECURITY: [auth-audit-panic-scrub] positive control: visible panic text was "+
			"destroyed on the way to the log (got %q)", got)
	}
}

// redPanicTokenStore is a MagicLinkTokenStore whose Cleanup panics with
// the probe payload — the sibling shape: the magic-link reaper's recover
// guard also logs the recovered value and must scrub it too.
type redPanicTokenStore struct{}

func (redPanicTokenStore) CreateToken(context.Context, string, time.Duration) (string, error) {
	return "tok", nil
}

func (redPanicTokenStore) RedeemToken(context.Context, string) (string, error) {
	return "", ErrTokenNotFound
}

func (redPanicTokenStore) Cleanup(context.Context) (int, error) {
	panic(redAuditPayload)
}

// TestMagicLinkReaperPanicScrubbed (sibling sweep): reapExpiredTokens
// logs a panicking host token store's recovered value through
// textsafe.Recovered, like the audit funnel and the register-notice
// goroutine.
func TestMagicLinkReaperPanicScrubbed(t *testing.T) {
	prev := slog.Default()
	sink := &redAuditScrubSink{}
	slog.SetDefault(slog.New(sink))
	t.Cleanup(func() { slog.SetDefault(prev) })

	plugin := &MagicLinkPlugin{tokenStore: redPanicTokenStore{}}
	plugin.reapExpiredTokens()

	got := sink.redGet("panic")
	if got == "" {
		t.Fatalf("setup broken: cleanup panic never reached the slog sink (attrs=%v)", sink.attrs)
	}
	for _, bad := range []string{"\x1b", "\u202e", "\u009b"} {
		if strings.Contains(got, bad) {
			t.Errorf("SECURITY: [auth-reaper-panic-scrub] recovered panic value reached slog "+
				"unscrubbed: raw %q present in panic attr %q — the reaper's recover guard is the "+
				"same terminal-injection surface as the audit funnel", bad, got)
		}
	}
	if !strings.Contains(got, "boom") {
		t.Errorf("SECURITY: [auth-reaper-panic-scrub] positive control: visible panic text was "+
			"destroyed on the way to the log (got %q)", got)
	}
}
