package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

type idUser string

func (u idUser) GetID() string { return string(u) }

func subscribeCall(t *testing.T, s *Server, ctx context.Context, method, uri string) {
	t.Helper()
	params, _ := json.Marshal(map[string]string{"uri": uri})
	resp := s.HandleRequest(ctx, Request{JSONRPC: "2.0", ID: 1, Method: method, Params: params})
	if resp.Error != nil {
		t.Fatalf("%s %s: %v", method, uri, resp.Error)
	}
}

// resources/unsubscribe releases only the caller's own subscription.
// The per-uri refcount was shared by every caller, so any caller past
// the server gate could send resources/unsubscribe for a uri it never
// subscribed to and switch off notifications/resources/updated for
// everyone who had: the count reached zero and NotifyResourceUpdated
// went quiet. Delivery stays connection-agnostic (the transport has no
// session id linking a POST to a GET stream), but releasing a
// subscription is the subscriber's act, not a stranger's.
func TestUnsubscribeReleasesOnlyOwn(t *testing.T) {
	s := NewServer()
	ts := newNotificationServer(t, s)
	events := openStream(t, ts, false)
	waitForSubscribers(t, s, 1)

	alice := handler.SetUser(context.Background(), idUser("alice"))
	mallory := handler.SetUser(context.Background(), idUser("mallory"))

	subscribeCall(t, s, alice, "resources/subscribe", "docs://x")
	// Mallory never subscribed; her unsubscribes must change nothing.
	subscribeCall(t, s, mallory, "resources/unsubscribe", "docs://x")
	subscribeCall(t, s, mallory, "resources/unsubscribe", "docs://x")

	s.NotifyResourceUpdated("docs://x")
	method, uri := decodeNotification(t, awaitSSE(t, events, "update after a stranger's unsubscribe"))
	if method != "notifications/resources/updated" || uri != "docs://x" {
		t.Fatalf("got %q uri %q, want notifications/resources/updated docs://x", method, uri)
	}

	// Alice's own unsubscribe still ends delivery.
	subscribeCall(t, s, alice, "resources/unsubscribe", "docs://x")
	s.NotifyResourceUpdated("docs://x")
	expectNoSSE(t, events, "update after the subscriber's own unsubscribe")
}

// Two callers subscribed to one uri: one leaving keeps the other's
// delivery, and a caller's repeated unsubscribe cannot release more than
// it subscribed.
func TestUnsubscribeKeepsOtherCallers(t *testing.T) {
	s := NewServer()
	ts := newNotificationServer(t, s)
	events := openStream(t, ts, false)
	waitForSubscribers(t, s, 1)

	alice := handler.SetUser(context.Background(), idUser("alice"))
	bob := handler.SetUser(context.Background(), idUser("bob"))
	subscribeCall(t, s, alice, "resources/subscribe", "docs://y")
	subscribeCall(t, s, bob, "resources/subscribe", "docs://y")
	subscribeCall(t, s, bob, "resources/unsubscribe", "docs://y")
	subscribeCall(t, s, bob, "resources/unsubscribe", "docs://y")

	s.NotifyResourceUpdated("docs://y")
	if method, _ := decodeNotification(t, awaitSSE(t, events, "update while alice is subscribed")); method != "notifications/resources/updated" {
		t.Fatalf("got %q, want notifications/resources/updated", method)
	}
}
