package openai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/provider"
)

// Pins the redirect-following default http.Client on the credential-bearing
// Chat fetch, found by the 2026-09-04 red-probe round; fixed by building the
// default client with CheckRedirect returning http.ErrUseLastResponse (the
// battery/auth/oidc.go oidcNoRedirect spelling), so a provider 3xx is
// answered as the final response instead of re-sending the bearer key.
// Family: F2 Outbound fetch allow-list (redirect re-check on
// credential-bearing provider fetches)
// Property: a credential-bearing provider fetch never follows a redirect —
// the client answers the 3xx as the final response, so the bearer key is
// delivered only to the origin the caller configured.
// Surfaces: internal/openai/client.go::Chat — the default http.Client built
// when Client.HTTP is nil. This one construction serves every provider that
// speaks the OpenAI wire format with a nil HTTP field: openrouter.Chat
// (openrouter.go), zai.Chat (zai.go), and bare openai.Client users.
// openrouter.Models builds its own twin (pinned in that package's
// models_security_test.go).

// TestChatDefaultClientRefusesRedirect serves a same-host 307 from
// /chat/completions to /sink and asserts the bearer key never reaches the
// redirect target. Same host is the worst case net/http actually forwards
// Authorization to.
func TestChatDefaultClientRefusesRedirect(t *testing.T) {
	var mu sync.Mutex
	var sinkAuth string
	var sinkHits int

	mux := http.NewServeMux()
	mux.HandleFunc("/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/sink", http.StatusTemporaryRedirect)
	})
	mux.HandleFunc("/sink", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		sinkHits++
		sinkAuth = r.Header.Get("Authorization")
		mu.Unlock()
		// A valid 200 SSE stream: with the redirect followed, Chat must
		// look like plain success — that silence is the leak.
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: [DONE]\n\n")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, APIKey: "sk-red-redirect", Name: "test"} // not-a-secret: test fixture, never a live credential
	ch, err := c.Chat(context.Background(), &provider.Request{Model: "m"})
	if err == nil {
		for range ch { // drain so the pump exits either way
		}
	}

	mu.Lock()
	hits, auth := sinkHits, sinkAuth
	mu.Unlock()
	if err == nil {
		t.Errorf("Chat followed the 307 (returned a stream, no error) instead of answering it as the final response")
	}
	if hits != 0 {
		t.Errorf("redirect target was fetched (%d hit(s)); the credential-bearing request must never be re-sent to it, Authorization seen: %q", hits, auth)
	}
}
