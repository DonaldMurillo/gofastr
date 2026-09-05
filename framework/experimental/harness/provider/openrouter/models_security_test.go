package openrouter

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/provider"
)

// Pins the redirect-following default client and the unbounded catalog
// decode on the credential-bearing /models fetch, found by the 2026-09-04
// red-probe round; fixed by building the default client with
// CheckRedirect returning http.ErrUseLastResponse (oidcNoRedirect
// spelling) and decoding through io.LimitReader(resp.Body, modelsMaxBody)
// (1 MiB, the battery/auth oauthProviderMaxBody convention).
// Family: F2 Outbound fetch allow-list (redirect re-check on
// credential-bearing provider fetches) + unbounded response-body decode on
// the same fetch.
// Property: the /models catalog fetch carries the API key as a bearer
// header, so it (a) never follows a redirect and (b) never buffers the
// response body to EOF unbounded.
// Surfaces: openrouter.go::Models — its own default http.Client and its
// catalog decode. The Chat path builds no client of its own (delegates to
// openai.Client with p.HTTP), so it inherits whatever the openai default
// does; TestChatRefusesRedirect pins it end-to-end so a future re-wiring
// cannot silently re-open it.

// TestModelsRefusesRedirect serves a same-host 307 at /models and asserts
// the bearer key never reaches the redirect target.
func TestModelsRefusesRedirect(t *testing.T) {
	var mu sync.Mutex
	var sinkAuth string
	var sinkHits int

	mux := http.NewServeMux()
	mux.HandleFunc("/models", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/sink", http.StatusTemporaryRedirect)
	})
	mux.HandleFunc("/sink", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		sinkHits++
		sinkAuth = r.Header.Get("Authorization")
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":[]}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	p := &Provider{APIKey: "sk-or-red-redirect", BaseURL: srv.URL} // not-a-secret: test fixture, never a live credential
	_, err := p.Models(context.Background())

	mu.Lock()
	hits, auth := sinkHits, sinkAuth
	mu.Unlock()
	if err == nil {
		t.Errorf("Models followed the 307 (returned a catalog, no error) instead of answering it as the final response")
	}
	if hits != 0 {
		t.Errorf("redirect target was fetched (%d hit(s)); the credential-bearing request must never be re-sent to it, Authorization seen: %q", hits, auth)
	}
}

// TestChatRefusesRedirect pins the same property through Provider.Chat,
// which delegates to the shared openai default client when Provider.HTTP
// is nil.
func TestChatRefusesRedirect(t *testing.T) {
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
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: [DONE]\n\n")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	p := &Provider{APIKey: "sk-or-red-redirect", BaseURL: srv.URL} // not-a-secret: test fixture, never a live credential
	ch, err := p.Chat(context.Background(), &provider.Request{Model: "m"})
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

// TestModelsCatalogDecodeIsBounded serves a valid but over-cap catalog
// body and asserts the decode fails on truncation instead of buffering
// the whole thing.
func TestModelsCatalogDecodeIsBounded(t *testing.T) {
	var body strings.Builder
	body.WriteString(`{"data":[{"id":"pad","name":"`)
	body.WriteString(strings.Repeat("N", 2<<20)) // 2 MiB: over the 1 MiB cap
	body.WriteString(`"}]}`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body.String()))
	}))
	defer srv.Close()

	p := &Provider{APIKey: "sk-or-red-body", BaseURL: srv.URL} // not-a-secret: test fixture, never a live credential
	models, err := p.Models(context.Background())
	if err == nil {
		t.Fatalf("Models decoded a 2 MiB catalog body with no size bound (got %d models); the fetch is credential-bearing, the decode must be capped and fail on truncation", len(models))
	}
}
