package providertest

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// CapturedChat records the one chat request a CaptureChatServer saw:
// the requested headers and the decoded request body.
type CapturedChat struct {
	Headers map[string]string
	Body    RequestBody
}

// CaptureChatServer serves a single chat request: it records the named
// request headers and the decoded JSON body, then answers with the
// stop+usage SSE stream. It replaces the identical capture handlers in
// the openrouter and zai request-shape tests.
func CaptureChatServer(t testing.TB, headerNames ...string) (url string, got *CapturedChat) {
	t.Helper()
	got = &CapturedChat{Headers: map[string]string{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, name := range headerNames {
			got.Headers[name] = r.Header.Get(name)
		}
		got.Body = DecodeRequestBody(t, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, StopUsageSSE(1, 1))
	}))
	t.Cleanup(srv.Close)
	return srv.URL, got
}

// AssertChatRequestBody checks the wire shape every provider adapter
// must send: the request's model, streaming mode, the system prompt as
// the first message, and the user text block as a user message. It
// replaces the identical assertion tails of the openrouter and zai
// request-shape tests.
func AssertChatRequestBody(t testing.TB, body RequestBody) {
	t.Helper()
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
