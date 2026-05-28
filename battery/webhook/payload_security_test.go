package webhook

import (
	"context"
	"strings"
	"testing"
)

func TestPublish_RejectsInvalidJSONPayload(t *testing.T) {
	t.Parallel()
	mgr := New(NewMemoryStore(), Options{AllowPrivateNetworks: true})
	if _, err := mgr.Subscribe(context.Background(), Subscriber{
		URL:    "https://example.com/hook",
		Secret: "x",
		Events: []string{"*"},
	}); err != nil {
		t.Fatal(err)
	}

	queued, err := mgr.Publish(context.Background(), "orders.created", []byte("{not-json"))
	if err == nil || queued != 0 {
		t.Fatalf("SECURITY: [webhook] Publish accepted malformed JSON payload (queued=%d err=%v). Attack: sends non-JSON bytes while declaring Content-Type: application/json.", queued, err)
	}
}

func TestPublish_RejectsOversizedPayload(t *testing.T) {
	t.Parallel()
	mgr := New(NewMemoryStore(), Options{AllowPrivateNetworks: true})
	if _, err := mgr.Subscribe(context.Background(), Subscriber{
		URL:    "https://example.com/hook",
		Secret: "x",
		Events: []string{"*"},
	}); err != nil {
		t.Fatal(err)
	}

	huge := []byte(`{"blob":"` + strings.Repeat("a", 10*1024*1024) + `"}`)
	queued, err := mgr.Publish(context.Background(), "orders.created", huge)
	if err == nil || queued != 0 {
		t.Fatalf("SECURITY: [webhook] Publish accepted %d-byte payload (queued=%d err=%v). Attack: unbounded queue/memory growth via oversized webhook payload.", len(huge), queued, err)
	}
}
