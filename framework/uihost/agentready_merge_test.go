package uihost

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAgentBundlePreservesUnsetFields(t *testing.T) {
	for _, card := range []*AgentCardConfig{nil, {Name: "Override"}} {
		name := "derived"
		if card != nil {
			name = "explicit"
		}
		t.Run(name, func(t *testing.T) {
			host := newAgentReadyHost(
				WithLLMsTxt("Original", "keep lede", []LLMsTxtSection{{Title: "Keep section", Links: []LLMsTxtLink{{Name: "Guide", URL: "/guide"}}}}),
				WithAgentCard(AgentCardConfig{Name: "Original", Version: "2.0.0", MCPEndpoint: "/mcp", Streaming: true}),
				WithAgentReady(AgentReadyConfig{Title: "Bundle", AgentCard: card}),
			)
			server := httptest.NewServer(host)
			t.Cleanup(server.Close)
			body, _ := getBody(t, server.URL+"/llms.txt")
			for _, text := range []string{"# Bundle", "keep lede", "## Keep section", "[Guide](/guide)"} {
				if !strings.Contains(body, text) {
					t.Errorf("llms.txt dropped %q: %s", text, body)
				}
			}
			body, _ = getBody(t, server.URL+"/.well-known/agent-card.json")
			var payload struct {
				Name         string
				Version      string
				Capabilities struct{ Streaming bool }
			}
			if err := json.Unmarshal([]byte(body), &payload); err != nil {
				t.Fatal(err)
			}
			wantName := "Bundle"
			if card != nil {
				wantName = "Override"
			}
			if payload.Name != wantName || payload.Version != "2.0.0" || !payload.Capabilities.Streaming || !strings.Contains(body, "/mcp") {
				t.Errorf("card lost granular fields or bundle precedence: %s", body)
			}
		})
	}
}
