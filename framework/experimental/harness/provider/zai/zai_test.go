package zai

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/provider"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/provider/providertest"
)

// TestChatRequestShape asserts the outbound request carries the
// bearer token, the Request.Model on the wire, the system prompt as
// the first OpenAI message, and the user text block as a user
// message. Unlike OpenRouter, ZAI must NOT send HTTP-Referer or
// X-Title.
func TestChatRequestShape(t *testing.T) {
	url, got := providertest.CaptureChatServer(t, "Authorization", "HTTP-Referer", "X-Title")
	p := &Provider{APIKey: "sk-test", BaseURL: url}
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

	if want := "Bearer sk-test"; got.Headers["Authorization"] != want {
		t.Errorf("Authorization = %q, want %q", got.Headers["Authorization"], want)
	}
	if got.Headers["HTTP-Referer"] != "" {
		t.Errorf("HTTP-Referer = %q; ZAI must not send it", got.Headers["HTTP-Referer"])
	}
	if got.Headers["X-Title"] != "" {
		t.Errorf("X-Title = %q; ZAI must not send it", got.Headers["X-Title"])
	}
	providertest.AssertChatRequestBody(t, got.Body)
}

// TestChatStreamingParse drains the event channel and asserts at
// least one KindTextDelta with the streamed text plus a terminal
// KindStop and a KindUsage carrying token counts.
func TestChatStreamingParse(t *testing.T) {
	providertest.AssertStreamingParse(t, func(baseURL string) provider.Provider {
		return &Provider{APIKey: "sk-test", BaseURL: baseURL}
	})
}

// TestChatHTTPError401: the shared client owns the 401 path; what is this
// adapter's own is the provider name the error carries.
func TestChatHTTPError401(t *testing.T) {
	providertest.AssertChatHTTPError401(t, func(baseURL string) provider.Provider {
		return &Provider{APIKey: "bad", BaseURL: baseURL}
	}, "zai")
}
