// Package openrouter implements the OpenRouter Provider.
//
// OpenRouter sits in front of dozens of upstream models; we speak the
// OpenAI Chat Completions wire format. Model catalog comes from
// /v1/models with TTL caching; pricing metadata feeds the cost
// dashboard. HTTP-Referer and X-Title are required by some upstream
// models — we set both unconditionally.
package openrouter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/harness/provider"
	"github.com/DonaldMurillo/gofastr/framework/harness/provider/internal/openai"
)

const (
	defaultBase = "https://openrouter.ai/api/v1"
)

// Provider is the OpenRouter Provider.
type Provider struct {
	APIKey string
	HTTP   *http.Client

	// BaseURL overrides the default endpoint (for tests / proxies).
	BaseURL string

	// Referer + Title are required by some upstream models on OpenRouter.
	// Defaults: "https://gofastr.dev/harness" and "gofastr-harness".
	Referer string
	Title   string

	// ModelCatalogTTL controls how long /models responses are cached.
	// Default 1h.
	ModelCatalogTTL time.Duration

	mu           sync.Mutex
	cachedModels []provider.Model
	catalogAt    time.Time
}

// Name implements provider.Provider.
func (p *Provider) Name() string { return "openrouter" }

// Chat implements provider.Provider.
func (p *Provider) Chat(ctx context.Context, req *provider.Request) (<-chan provider.StreamEvent, error) {
	client := &openai.Client{
		BaseURL: p.baseURL(),
		APIKey:  p.APIKey,
		HTTP:    p.HTTP,
		Name:    p.Name(),
		Headers: map[string]string{
			"HTTP-Referer": p.referer(),
			"X-Title":      p.title(),
		},
	}
	return client.Chat(ctx, req)
}

// Models implements provider.Provider with TTL caching.
func (p *Provider) Models(ctx context.Context) ([]provider.Model, error) {
	p.mu.Lock()
	ttl := p.ModelCatalogTTL
	if ttl == 0 {
		ttl = time.Hour
	}
	if p.cachedModels != nil && time.Since(p.catalogAt) < ttl {
		out := append([]provider.Model{}, p.cachedModels...)
		p.mu.Unlock()
		return out, nil
	}
	p.mu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL()+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.APIKey)
	client := p.HTTP
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		return nil, fmt.Errorf("openrouter /models: HTTP %d: %s", resp.StatusCode, body)
	}

	var parsed struct {
		Data []struct {
			ID            string `json:"id"`
			Name          string `json:"name"`
			ContextLength int    `json:"context_length"`
			Pricing       struct {
				Prompt     string `json:"prompt"`
				Completion string `json:"completion"`
			} `json:"pricing"`
			TopProvider struct {
				MaxCompletionTokens int `json:"max_completion_tokens"`
			} `json:"top_provider"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("openrouter /models: parse: %w", err)
	}
	out := make([]provider.Model, 0, len(parsed.Data))
	for _, d := range parsed.Data {
		out = append(out, provider.Model{
			ID:            d.ID,
			Name:          d.Name,
			ContextWindow: d.ContextLength,
			MaxOutput:     d.TopProvider.MaxCompletionTokens,
			Pricing: provider.Pricing{
				InputPerMTok:  parseUSDPerToken(d.Pricing.Prompt),
				OutputPerMTok: parseUSDPerToken(d.Pricing.Completion),
			},
			Capabilities: provider.Capabilities{ToolUse: true},
		})
	}
	p.mu.Lock()
	p.cachedModels = out
	p.catalogAt = time.Now()
	p.mu.Unlock()
	return append([]provider.Model{}, out...), nil
}

// TokenCount returns a best-effort approximation. OpenRouter doesn't
// have a tokenization endpoint; we fall back to a 4-chars-per-token
// heuristic (the rate of thumb across English text for most modern
// tokenizers).
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
	return defaultBase
}

func (p *Provider) referer() string {
	if p.Referer != "" {
		return p.Referer
	}
	return "https://gofastr.dev/harness"
}

func (p *Provider) title() string {
	if p.Title != "" {
		return p.Title
	}
	return "gofastr-harness"
}

// OpenRouter prices come as strings in USD-per-token like "0.000003".
// We convert to USD-per-million-tokens. Returns 0 on parse failure
// (better than crashing the catalog).
func parseUSDPerToken(s string) float64 {
	if s == "" {
		return 0
	}
	var v float64
	_, err := fmt.Sscanf(s, "%g", &v)
	if err != nil {
		return 0
	}
	return v * 1_000_000
}

// ErrMissingKey is returned when the Provider is asked to chat without an API key.
var ErrMissingKey = errors.New("openrouter: API key not set")
