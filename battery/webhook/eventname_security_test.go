package webhook

import (
	"context"
	"testing"
)

// assertPublishRefused drives one "Publish must refuse this input"
// property shared by the publish security tests: a wildcard subscriber is
// listening (so queued=0 proves refusal, not an empty subscriber list)
// and Publish must both error and queue nothing.
func assertPublishRefused(t *testing.T, event string, payload []byte, what, attack string) {
	t.Helper()
	mgr := New(NewMemoryStore(), Options{AllowPrivateNetworks: true})
	if _, err := mgr.Subscribe(context.Background(), Subscriber{
		URL:    "https://example.com/hook",
		Secret: "x",
		Events: []string{"*"},
	}); err != nil {
		t.Fatal(err)
	}

	queued, err := mgr.Publish(context.Background(), event, payload)
	if err == nil || queued != 0 {
		t.Fatalf("SECURITY: [webhook] Publish accepted %s (queued=%d err=%v). Attack: %s.", what, queued, err, attack)
	}
}

func TestPublish_RejectsCRLFEventName(t *testing.T) {
	t.Parallel()
	assertPublishRefused(t, "orders.created\r\nX-Evil: 1", []byte("{}"),
		"CR/LF in event name",
		"outbound header injection / poisoned delivery retries via X-GoFastr-Event")
}

func TestPublish_RejectsNULEventName(t *testing.T) {
	t.Parallel()
	assertPublishRefused(t, "orders.created\x00admin", []byte("{}"),
		"NUL in event name",
		"invalid outbound header value / poison-pill delivery record")
}

func TestSubscribe_RejectsCRLFEventPattern(t *testing.T) {
	t.Parallel()
	mgr := New(NewMemoryStore(), Options{AllowPrivateNetworks: true})

	_, err := mgr.Subscribe(context.Background(), Subscriber{
		URL:    "https://example.com/hook",
		Secret: "x",
		Events: []string{"orders.*\r\nadmin.*"},
	})
	if err == nil {
		t.Fatalf("SECURITY: [webhook] Subscribe accepted CR/LF in event pattern. Attack: hidden subscription pattern / registry poisoning with control characters.")
	}
}

func TestSubscribe_RejectsNULEventPattern(t *testing.T) {
	t.Parallel()
	mgr := New(NewMemoryStore(), Options{AllowPrivateNetworks: true})

	_, err := mgr.Subscribe(context.Background(), Subscriber{
		URL:    "https://example.com/hook",
		Secret: "x",
		Events: []string{"orders.*\x00admin.*"},
	})
	if err == nil {
		t.Fatalf("SECURITY: [webhook] Subscribe accepted NUL in event pattern. Attack: invisible subscriber filter poisoning / audit confusion.")
	}
}
