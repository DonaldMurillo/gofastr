package webhook

import (
	"context"
	"testing"
)

// ============================================================================
// Event-name C1 controls — pinned 2026-09-05 (adversarial pass round 4;
// promoted from that round's red probe once validateEventName covered the
// full core/textsafe unsafe set).
// Family: F25 Bidi, invisible, and confusable characters (C1 control
// characters, 8-bit form).
// Property: an event-name guard whose documented purpose is to stop
// control characters reaching outbound headers and persisted rows must
// reject ALL Unicode control characters, including the C1 range
// U+0080–U+009F, not only C0 and DEL.
// Surfaces: webhook.go validateEventName (the guard itself, reached from
// Manager.Publish for event names and Manager.Subscribe for event
// patterns), webhook.go attempt (the sink: req.Header.Set("X-GoFastr-
// Event", d.Event) on every delivery POST), sql.go delivery rows,
// inbound X-GoFastr-Event echoes. The C0 subset is pinned by
// eventname_security_test.go; this file pins the C1 remainder.
// ============================================================================

// TestEventNameRejectsC1Controls asserts the guard rejects C1 controls at both surfaces
// that accept event names (Publish names, Subscribe patterns) and at the guard itself.
func TestEventNameRejectsC1Controls(t *testing.T) {
	shapes := []string{
		"orders.paid\u009b[2J", // CSI: 8-bit ANSI escape introducer + erase-screen payload
		"orders.paid\u0085",    // NEL: C1 next-line
		"orders.paid\u009c",    // ST: string terminator, closes OSC sequences
	}
	for _, name := range shapes {
		if err := validateEventName(name); err == nil {
			t.Errorf("SECURITY: [webhook] validateEventName accepted C1 control %q: the guard's own contract "+
				"is \"no control characters into outbound headers / persisted rows\", and U+0080–U+009F are controls", name)
		}
	}

	mgr := New(NewMemoryStore(), Options{AllowPrivateNetworks: true})
	if _, err := mgr.Subscribe(context.Background(), Subscriber{
		URL: "https://example.com/hook", Secret: "x", Events: []string{"*"},
	}); err != nil {
		t.Fatalf("setup subscribe: %v", err)
	}
	if queued, err := mgr.Publish(context.Background(), "orders.paid\u009b[2J", []byte(`{}`)); err == nil || queued != 0 {
		t.Errorf("SECURITY: [webhook] Publish queued an event name carrying U+009B (queued=%d err=%v): it is "+
			"persisted on the delivery row and written raw to the outbound X-GoFastr-Event header on every retry "+
			"(Go permits obs-text bytes in header values), a terminal-escape injection at any receiver that renders it",
			queued, err)
	}
	if _, err := mgr.Subscribe(context.Background(), Subscriber{
		URL:    "https://example.com/hook",
		Secret: "x",
		Events: []string{"orders.\u009b"},
	}); err == nil {
		t.Errorf("SECURITY: [webhook] Subscribe accepted an event pattern carrying U+009B: an invisible control " +
			"character is persisted in the subscription registry")
	}
}
