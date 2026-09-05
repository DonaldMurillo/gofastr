package hook

import (
	"strings"
	"testing"
	"time"
)

// Property: a NEGATIVE caller-supplied duration must never fold onto the
// default arm of a `<= 0` check. runOne tested h.Timeout `<= 0` and
// substituted DefaultTimeout(event), so a hook registered with a negative
// timeout (sign or unit error, e.g. a TOML timeout_ms computed negative)
// silently got the LONGEST per-event default instead of the shortest
// deadline the caller asked for.
// Surfaces: Runner.Register / runOne (h.Timeout).
// Pins h.Timeout <= 0 folding onto DefaultTimeout, found by the
// 2026-09-04 red-probe round; fixed in Register rejecting h.Timeout < 0
// (runOne's only entry) while 0 keeps the per-event default.

func TestHookRegisterNegativeTimeoutRejected(t *testing.T) {
	r := New()
	err := r.Register(Hook{Event: EventPreToolUse, Command: "true", Timeout: -time.Second})
	if err == nil || !strings.Contains(err.Error(), "Timeout") {
		t.Fatalf("Register: negative hook Timeout silently folded onto the per-event default: %v", err)
	}
}

func TestHookRegisterZeroTimeoutKeepsDefault(t *testing.T) {
	r := New()
	if err := r.Register(Hook{Event: EventPreToolUse, Command: "true"}); err != nil {
		t.Fatalf("zero Timeout must keep DefaultTimeout: %v", err)
	}
	if hooks := r.HooksFor(EventPreToolUse); len(hooks) != 1 {
		t.Fatalf("zero-timeout hook must register, got %d hooks", len(hooks))
	}
}
