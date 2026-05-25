package copilot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/harness/provider"
)

// stub Copilot internal API: exchange endpoint + chat endpoint.
func stubExchange(t *testing.T, chatBase string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "token gh-test" {
			t.Errorf("bad GH auth: %q", r.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token":      "internal-tok",
			"expires_at": time.Now().Add(30 * time.Minute).Unix(),
			"endpoints":  map[string]any{"api": chatBase},
		})
	}))
}

func stubChat(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer internal-tok") {
			t.Errorf("bad internal auth: %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Editor-Version") == "" {
			t.Errorf("missing Editor-Version")
		}
		if r.Header.Get("Copilot-Integration-Id") == "" {
			t.Errorf("missing Copilot-Integration-Id")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		// One TextDelta + DONE.
		_, _ = w.Write([]byte(`data: {"choices":[{"delta":{"content":"hello copilot"}}]}
data: [DONE]

`))
	}))
}

func TestCopilotChatIntegration(t *testing.T) {
	chat := stubChat(t)
	defer chat.Close()
	exchange := stubExchange(t, chat.URL)
	defer exchange.Close()

	// Swap the exchange URL via a small indirection: shadow
	// exchangeURL by using a Provider that points at chat.URL with
	// an override path. The simplest: stub by setting the apiBase
	// directly and giving a forged internal token.
	p := &Provider{
		GHToken:       "gh-test",
		EditorVersion: "test-editor/0.1",
		HTTP:          chat.Client(),
	}
	p.apiBase = chat.URL
	p.intTok = "internal-tok"
	p.intExpAt = time.Now().Add(time.Hour)

	stream, err := p.Chat(context.Background(), &provider.Request{Model: "claude-4"})
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	for ev := range stream {
		if ev.Kind == provider.KindTextDelta {
			out.WriteString(ev.Text)
		}
	}
	if out.String() != "hello copilot" {
		t.Errorf("text = %q", out.String())
	}
}

func TestCopilotExchangeFlow(t *testing.T) {
	chat := stubChat(t)
	defer chat.Close()
	exchange := stubExchange(t, chat.URL)
	defer exchange.Close()

	p := &Provider{
		GHToken: "gh-test",
		HTTP:    chat.Client(),
	}
	// Point the exchange URL at our stub by issuing a manual exchange.
	req, _ := http.NewRequest(http.MethodGet, exchange.URL, nil)
	req.Header.Set("Authorization", "token gh-test")
	req.Header.Set("Editor-Version", p.editorVersion())
	req.Header.Set("Copilot-Integration-Id", p.integrationID())
	resp, err := p.httpDo(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var parsed struct {
		Token     string `json:"token"`
		Endpoints struct {
			API string `json:"api"`
		} `json:"endpoints"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Token != "internal-tok" {
		t.Errorf("token = %q", parsed.Token)
	}
	if parsed.Endpoints.API != chat.URL {
		t.Errorf("endpoint.api = %q", parsed.Endpoints.API)
	}
}

func TestCopilotChatNoGHToken(t *testing.T) {
	p := &Provider{}
	if _, err := p.Chat(context.Background(), &provider.Request{}); err == nil {
		t.Fatal("expected error when GHToken is unset")
	}
}
