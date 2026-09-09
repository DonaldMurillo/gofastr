package openrouter

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/provider"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/provider/providertest"
)

// TestChatRequestShape asserts the outbound request carries the
// bearer token, the Request.Model on the wire, the system prompt as
// the first OpenAI message, and the user text block as a user
// message. OpenRouter additionally sends HTTP-Referer and X-Title.
func TestChatRequestShape(t *testing.T) {
	var gotAuth, gotReferer, gotTitle string
	var body providertest.RequestBody
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotReferer = r.Header.Get("HTTP-Referer")
		gotTitle = r.Header.Get("X-Title")
		body = providertest.DecodeRequestBody(t, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, providertest.StopUsageSSE(1, 1))
	}))
	defer srv.Close()

	p := &Provider{APIKey: "sk-test", BaseURL: srv.URL}
	ch, err := p.Chat(context.Background(), &provider.Request{
		Model:  "m",
		System: "sys",
		Messages: []provider.Message{{
			Role:    provider.RoleUser,
			Content: []control.ContentBlock{{Type: "text", Text: "hello world"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for range ch {
	}

	if want := "Bearer sk-test"; gotAuth != want {
		t.Errorf("Authorization = %q, want %q", gotAuth, want)
	}
	if gotReferer == "" {
		t.Errorf("HTTP-Referer header missing; OpenRouter requires it")
	}
	if gotTitle == "" {
		t.Errorf("X-Title header missing; OpenRouter requires it")
	}
	if body.Model != "m" {
		t.Errorf("body.model = %q, want %q", body.Model, "m")
	}
	if !body.Stream {
		t.Errorf("body.stream = false, want true (streaming request)")
	}
	if len(body.Messages) != 2 {
		t.Fatalf("messages len = %d, want 2: %+v", len(body.Messages), body.Messages)
	}
	if body.Messages[0].Role != "system" || body.Messages[0].Content != "sys" {
		t.Errorf("messages[0] = {role:%q content:%q}, want {system sys}",
			body.Messages[0].Role, body.Messages[0].Content)
	}
	if body.Messages[1].Role != "user" || body.Messages[1].Content != "hello world" {
		t.Errorf("messages[1] = {role:%q content:%q}, want {user hello world}",
			body.Messages[1].Role, body.Messages[1].Content)
	}
}

// TestChatStreamingParse drains the event channel and asserts at
// least one KindTextDelta with the streamed text plus a terminal
// KindStop and a KindUsage carrying token counts.
func TestChatStreamingParse(t *testing.T) {
	providertest.AssertStreamingParse(t, func(baseURL string) provider.Provider {
		return &Provider{APIKey: "sk-test", BaseURL: baseURL}
	})
}

// TestChatHTTPError401 asserts a 401 response surfaces as an error
// whose message contains "401" (mirrors client_test.go).
func TestChatHTTPError401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("invalid key"))
	}))
	defer srv.Close()

	p := &Provider{APIKey: "bad", BaseURL: srv.URL}
	_, err := p.Chat(context.Background(), &provider.Request{Model: "m"})
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("err = %v, want error containing 401", err)
	}
}
