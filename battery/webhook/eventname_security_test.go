package webhook

import (
	"context"
	"testing"
)

func TestPublish_RejectsCRLFEventName(t *testing.T) {
	t.Parallel()
	mgr := New(NewMemoryStore(), Options{AllowPrivateNetworks: true})
	if _, err := mgr.Subscribe(context.Background(), Subscriber{
		URL:    "https://example.com/hook",
		Secret: "x",
		Events: []string{"*"},
	}); err != nil {
		t.Fatal(err)
	}

	queued, err := mgr.Publish(context.Background(), "orders.created\r\nX-Evil: 1", []byte("{}"))
	if err == nil || queued != 0 {
		t.Fatalf("SECURITY: [webhook] Publish accepted CR/LF in event name (queued=%d err=%v). Attack: outbound header injection / poisoned delivery retries via X-GoFastr-Event.", queued, err)
	}
}

func TestPublish_RejectsNULEventName(t *testing.T) {
	t.Parallel()
	mgr := New(NewMemoryStore(), Options{AllowPrivateNetworks: true})
	if _, err := mgr.Subscribe(context.Background(), Subscriber{
		URL:    "https://example.com/hook",
		Secret: "x",
		Events: []string{"*"},
	}); err != nil {
		t.Fatal(err)
	}

	queued, err := mgr.Publish(context.Background(), "orders.created\x00admin", []byte("{}"))
	if err == nil || queued != 0 {
		t.Fatalf("SECURITY: [webhook] Publish accepted NUL in event name (queued=%d err=%v). Attack: invalid outbound header value / poison-pill delivery record.", queued, err)
	}
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
