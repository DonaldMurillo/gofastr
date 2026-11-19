package zai

import (
	"context"
	"net/http"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/provider"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/provider/internal/openai"
)

const (
	defaultBase       = "https://api.z.ai/api/paas/v4"
	defaultCodingBase = "https://api.z.ai/api/coding/paas/v4"
)

// Provider is the ZAI GLM provider; CodingPlan selects the GLM Coding Plan endpoint, and BaseURL, when set, wins over both.
type Provider struct {
	APIKey     string
	HTTP       *http.Client
	BaseURL    string
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

// Models implements provider.Provider with a static catalog.
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

// TokenCount implements provider.Provider with provider.EstimateTokens.
func (p *Provider) TokenCount(_ context.Context, _ string, msgs []provider.Message) (int, error) {
	return provider.EstimateTokens(msgs), nil
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
