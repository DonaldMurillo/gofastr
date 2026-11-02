package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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

// The per-caller subscription record is bounded as a whole, not only
// per caller. An anonymous caller's identity is its TCP peer host, so a
// client with many addresses (an IPv6 prefix) mints a fresh caller per
// address; subscribe needs no open stream, and the records are cleared
// only when the last stream leaves, so with no stream they were never
// cleared. Recording every (caller, uri) pair grew without end:
// 5000 peers × 10 uris held 50000 entries where the shared per-uri
// count alone held 10. Past maxResourceSubscriptionPairs a subscribe
// that would add a pair is refused, the way one past the distinct-uri
// cap is.
func TestSubscriptionRecordBounded(t *testing.T) {
	s := NewServer()
	refused := 0
	for i := range maxResourceSubscriptionPairs + 500 {
		r := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		r.RemoteAddr = fmt.Sprintf("[2001:db8::%x]:4000", i+1)
		params, _ := json.Marshal(map[string]string{"uri": fmt.Sprintf("docs://%d", i%10)})
		resp := s.HandleRequest(WithRequest(context.Background(), r),
			Request{JSONRPC: "2.0", ID: 1, Method: "resources/subscribe", Params: params})
		if resp.Error != nil {
			refused++
		}
	}
	s.sseMu.Lock()
	pairs, uris := len(s.resourceSubsBy), len(s.resourceSubs)
	s.sseMu.Unlock()
	if pairs > maxResourceSubscriptionPairs {
		t.Fatalf("SECURITY: [dos] %d caller/uri subscriptions retained, want at most %d", pairs, maxResourceSubscriptionPairs)
	}
	if refused == 0 {
		t.Fatal("no subscribe was refused past the bound")
	}
	if uris != 10 {
		t.Fatalf("distinct uris = %d, want 10", uris)
	}

	// A caller already holding the pair still re-subscribes at the cap:
	// only growth of the record is bounded.
	r := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	r.RemoteAddr = "[2001:db8::1]:4000"
	params, _ := json.Marshal(map[string]string{"uri": "docs://0"})
	if resp := s.HandleRequest(WithRequest(context.Background(), r),
		Request{JSONRPC: "2.0", ID: 1, Method: "resources/subscribe", Params: params}); resp.Error != nil {
		t.Fatalf("re-subscribe of a held pair refused at the cap: %v", resp.Error)
	}
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
