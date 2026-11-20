package openrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/textsafe"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/provider"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/provider/internal/openai"
)

const (
	defaultBase   = "https://openrouter.ai/api/v1"
	modelsMaxBody = 1 << 20
)

// Provider is the OpenRouter provider. Referer and Title are sent as HTTP-Referer and X-Title.
type Provider struct {
	APIKey  string
	HTTP    *http.Client
	BaseURL string
	Referer string
	Title   string

	// ModelCatalogTTL is how long a /models answer is cached; zero means one hour.
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
		client = &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		return nil, fmt.Errorf("openrouter /models: HTTP %d: %s", resp.StatusCode, textsafe.StripUnsafe(string(body)))
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
	rawModels, err := io.ReadAll(io.LimitReader(resp.Body, modelsMaxBody))
	if err != nil {
		return nil, fmt.Errorf("openrouter /models: read: %w", err)
	}
	if err := handler.CheckObjectKeys(rawModels, handler.FoldJSONKey); err != nil {
		return nil, fmt.Errorf("openrouter /models: parse: %w", err)
	}
	if err := json.Unmarshal(rawModels, &parsed); err != nil {
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

// TokenCount implements provider.Provider with provider.EstimateTokens.
func (p *Provider) TokenCount(_ context.Context, _ string, msgs []provider.Message) (int, error) {
	return provider.EstimateTokens(msgs), nil
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
