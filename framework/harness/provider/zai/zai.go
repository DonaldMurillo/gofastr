// Package zai implements the ZAI GLM Provider (OpenAI-compatible).
//
// Endpoint: https://api.z.ai/api/paas/v4/chat/completions
// Models:   glm-4.6, glm-4.5-air, glm-z1
//
// See docs/harness-architecture.md § Providers → ZAI GLM.
package zai

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/harness/provider"
	"github.com/DonaldMurillo/gofastr/framework/harness/provider/internal/openai"
)

// ZAI publishes two base URLs:
//
//   - https://api.z.ai/api/paas/v4 — general API (pay-as-you-go).
//   - https://api.z.ai/api/coding/paas/v4 — GLM Coding Plan
//     subscription. Same wire shape, dedicated quota.
//
// Keys provisioned for the Coding Plan return HTTP 429 with code
// 1113 "Insufficient balance" if you hit the general endpoint
// instead. The Provider auto-detects via the CodingPlan flag (default
// false → general endpoint).
const (
	defaultBase       = "https://api.z.ai/api/paas/v4"
	defaultCodingBase = "https://api.z.ai/api/coding/paas/v4"
)

// Provider is the ZAI GLM Provider.
type Provider struct {
	APIKey  string
	HTTP    *http.Client
	BaseURL string // explicit override (wins over CodingPlan)

	// CodingPlan switches the base URL to the GLM Coding Plan
	// endpoint. Honored when BaseURL is empty.
	CodingPlan bool
}

// Name implements provider.Provider.
func (p *Provider) Name() string { return "zai" }

// Chat implements provider.Provider.
func (p *Provider) Chat(ctx context.Context, req *provider.Request) (<-chan provider.StreamEvent, error) {
	client := &openai.Client{
		BaseURL: p.baseURL(),
		APIKey:  p.APIKey,
		HTTP:    p.HTTP,
		Name:    p.Name(),
	}
	return client.Chat(ctx, req)
}

// Models implements provider.Provider with the v0.1 static catalog.
// ZAI doesn't publish a discoverable /models endpoint in their public
// docs, so the catalog is hardcoded. When ZAI publishes a model
// catalog API, switch to dynamic discovery.
//
// GLM-5.1 is the current flagship and is listed first so default
// pickers land on it; older models stay in the catalog for users who
// have prompts tuned to specific versions.
func (p *Provider) Models(_ context.Context) ([]provider.Model, error) {
	return []provider.Model{
		{
			ID:            "glm-5.1",
			Name:          "GLM-5.1",
			ContextWindow: 128_000,
			MaxOutput:     16_384,
			Capabilities:  provider.Capabilities{ToolUse: true, Thinking: true, Vision: true},
		},
		{
			ID:            "glm-4.6",
			Name:          "GLM-4.6",
			ContextWindow: 128_000,
			MaxOutput:     8192,
			Capabilities:  provider.Capabilities{ToolUse: true},
		},
		{
			ID:            "glm-4.5-air",
			Name:          "GLM-4.5 Air",
			ContextWindow: 128_000,
			MaxOutput:     8192,
			Capabilities:  provider.Capabilities{ToolUse: true},
		},
		{
			ID:            "glm-z1",
			Name:          "GLM-Z1",
			ContextWindow: 128_000,
			MaxOutput:     8192,
			Capabilities:  provider.Capabilities{ToolUse: true, Thinking: true},
		},
	}, nil
}

// TokenCount uses the same 4-chars-per-token heuristic as OpenRouter.
func (p *Provider) TokenCount(_ context.Context, _ string, msgs []provider.Message) (int, error) {
	total := 0
	for _, m := range msgs {
		for _, b := range m.Content {
			total += len(b.Text)
			if b.ToolUse != nil {
				total += len(b.ToolUse.Name) + len(b.ToolUse.Input)
			}
			if b.ToolResult != nil {
				for _, c := range b.ToolResult.Content {
					total += len(c.Text)
				}
			}
		}
	}
	return (total + 3) / 4, nil
}

func (p *Provider) baseURL() string {
	if p.BaseURL != "" {
		return p.BaseURL
	}
	if p.CodingPlan {
		return defaultCodingBase
	}
	return defaultBase
}

// Default HTTP client with a generous timeout for slow GLM responses.
func defaultClient() *http.Client {
	return &http.Client{Timeout: 5 * time.Minute}
}

// ErrMissingKey is returned when the Provider is asked to chat without an API key.
var ErrMissingKey = errors.New("zai: API key not set")
