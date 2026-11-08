package webhook

import (
	"context"
	"testing"
)

func TestSubscriberRefusesUnstorableText(t *testing.T) {
	_, store := openSQLStore(t)
	manager := New(store, Options{AllowPrivateNetworks: true})
	for _, invalid := range []string{"bad\x00text", "bad\xfftext", "bad\xe2\x82"} {
		for _, field := range []string{"url", "secret", "id"} {
			sub := Subscriber{ID: "safe", URL: "https://8.8.8.8/hook", Secret: "test-signing-key"}
			switch field {
			case "url":
				sub.URL += invalid
			case "secret":
				sub.Secret = invalid
			case "id":
				sub.ID = invalid
			}
			if _, err := manager.Subscribe(context.Background(), sub); err == nil {
				t.Errorf("accepted unstorable %s %q", field, invalid)
			}
		}
	}
	subscribers, err := store.ListSubscribers(context.Background())
	if err != nil || len(subscribers) != 0 {
		t.Fatalf("invalid subscribers persisted: %d %v", len(subscribers), err)
	}
	if _, err := manager.Subscribe(context.Background(), Subscriber{ID: "café", URL: "https://8.8.8.8/café", Secret: "clé"}); err != nil {
		t.Errorf("valid Unicode refused: %v", err)
	}
}

func TestUnsubscribeRefusesUnstorableID(t *testing.T) {
	manager := New(NewMemoryStore(), Options{})
	for _, id := range []string{"bad\x00id", "bad\xffid"} {
		if err := manager.Unsubscribe(context.Background(), id); err == nil {
			t.Errorf("accepted unstorable id %q", id)
		}
	}
}
