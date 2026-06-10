// Package copilot implements the GitHub Copilot Provider.
//
// Copilot's chat endpoint is reverse-engineered from public clients
// (copilot.vim, copilot-language-server). The wire shape is
// OpenAI-compatible Chat Completions; the auth flow is the
// distinguishing piece:
//
//  1. Device-code OAuth against github.com → user-facing code
//  2. Poll for GH access token
//  3. Exchange GH token at api.github.com/copilot_internal/v2/token
//     for a short-lived Copilot internal token (refresh via the
//     same endpoint when it expires)
//  4. Use the Copilot token against api.githubcopilot.com with
//     Editor-Version + Copilot-Integration-Id headers
//
// The exchange response includes `endpoints.api` which we respect —
// GitHub has moved this for some users.
package copilot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/harness/provider"
	"github.com/DonaldMurillo/gofastr/framework/harness/provider/internal/openai"
)

const (
	// DefaultClientID is the public Copilot OAuth client ID.
	DefaultClientID = "Iv1.b507a08c87ecfe98"

	deviceCodeURL  = "https://github.com/login/device/code"
	accessTokenURL = "https://github.com/login/oauth/access_token"
	exchangeURL    = "https://api.github.com/copilot_internal/v2/token"
	defaultAPIBase = "https://api.githubcopilot.com"

	// IntegrationIDs that Copilot's server accepts. We default to
	// vscode-chat which is the most permissive; the harness can
	// override via Provider.IntegrationID.
	defaultIntegrationID = "vscode-chat"
)

// Provider is the GitHub Copilot Provider.
type Provider struct {
	// GHToken is the GitHub access token obtained via device-code
	// OAuth. Stored in credstore under (provider="copilot",
	// account="github"); the harness loads it before calling Chat.
	GHToken string

	// IntegrationID overrides the default ("vscode-chat"). Other
	// known-good values: "vscode" / "copilot-chat" / "jetbrains".
	IntegrationID string

	// EditorVersion is the User-Agent-like string Copilot expects.
	EditorVersion string

	HTTP *http.Client

	mu       sync.Mutex
	apiBase  string // populated from exchange response; falls back to defaultAPIBase
	intTok   string // current internal token
	intExpAt time.Time
}

// Name implements provider.Provider.
func (p *Provider) Name() string { return "copilot" }

// Chat implements provider.Provider.
func (p *Provider) Chat(ctx context.Context, req *provider.Request) (<-chan provider.StreamEvent, error) {
	if p.GHToken == "" {
		return nil, ErrNoGHToken
	}
	tok, base, err := p.ensureInternalToken(ctx)
	if err != nil {
		return nil, err
	}
	client := &openai.Client{
		BaseURL: base,
		APIKey:  tok,
		HTTP:    p.HTTP,
		Name:    p.Name(),
		Headers: map[string]string{
			"Editor-Version":         p.editorVersion(),
			"Copilot-Integration-Id": p.integrationID(),
			"X-GitHub-Api-Version":   "2025-01-01",
		},
	}
	return client.Chat(ctx, req)
}

// Models implements provider.Provider. Catalog comes from
// /models on the Copilot API base. The catalog returned by Copilot
// depends on the user's subscription tier and per-org policy.
func (p *Provider) Models(ctx context.Context) ([]provider.Model, error) {
	tok, base, err := p.ensureInternalToken(ctx)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Editor-Version", p.editorVersion())
	req.Header.Set("Copilot-Integration-Id", p.integrationID())
	resp, err := p.httpDo(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		return nil, fmt.Errorf("copilot /models: HTTP %d: %s", resp.StatusCode, body)
	}
	var parsed struct {
		Data []struct {
			ID            string `json:"id"`
			Name          string `json:"name"`
			ContextWindow int    `json:"context_window"`
			MaxOutput     int    `json:"max_output_tokens"`
			Capabilities  struct {
				Vision   bool `json:"vision"`
				Thinking bool `json:"thinking"`
				ToolUse  bool `json:"tools"`
			} `json:"capabilities"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	out := make([]provider.Model, 0, len(parsed.Data))
	for _, d := range parsed.Data {
		out = append(out, provider.Model{
			ID:            d.ID,
			Name:          d.Name,
			ContextWindow: d.ContextWindow,
			MaxOutput:     d.MaxOutput,
			Capabilities: provider.Capabilities{
				Vision:   d.Capabilities.Vision,
				Thinking: d.Capabilities.Thinking,
				ToolUse:  d.Capabilities.ToolUse,
			},
		})
	}
	return out, nil
}

// TokenCount uses the same heuristic as the OpenAI-compatible adapters.
func (p *Provider) TokenCount(_ context.Context, _ string, msgs []provider.Message) (int, error) {
	total := 0
	for _, m := range msgs {
		for _, b := range m.Content {
			total += len(b.Text)
		}
	}
	return (total + 3) / 4, nil
}

// ---------- internal token exchange ----------

// ensureInternalToken returns a current Copilot internal token,
// refreshing if expired. Returns (token, apiBase).
func (p *Provider) ensureInternalToken(ctx context.Context) (string, string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.intTok != "" && time.Now().Before(p.intExpAt.Add(-1*time.Minute)) {
		return p.intTok, p.apiBaseLocked(), nil
	}
	if err := p.refreshInternalTokenLocked(ctx); err != nil {
		return "", "", err
	}
	return p.intTok, p.apiBaseLocked(), nil
}

func (p *Provider) apiBaseLocked() string {
	if p.apiBase != "" {
		return p.apiBase
	}
	return defaultAPIBase
}

func (p *Provider) refreshInternalTokenLocked(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, exchangeURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "token "+p.GHToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Editor-Version", p.editorVersion())
	req.Header.Set("Copilot-Integration-Id", p.integrationID())
	resp, err := p.httpDo(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("copilot token exchange: HTTP %d: %s", resp.StatusCode, body)
	}
	var parsed struct {
		Token     string `json:"token"`
		ExpiresAt int64  `json:"expires_at"`
		Endpoints struct {
			API string `json:"api"`
		} `json:"endpoints"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return err
	}
	p.intTok = parsed.Token
	p.intExpAt = time.Unix(parsed.ExpiresAt, 0)
	if parsed.Endpoints.API != "" {
		p.apiBase = strings.TrimRight(parsed.Endpoints.API, "/")
	} else {
		p.apiBase = defaultAPIBase
	}
	return nil
}

func (p *Provider) httpDo(req *http.Request) (*http.Response, error) {
	c := p.HTTP
	if c == nil {
		c = &http.Client{Timeout: 30 * time.Second}
	}
	return c.Do(req)
}

func (p *Provider) editorVersion() string {
	if p.EditorVersion != "" {
		return p.EditorVersion
	}
	return "gofastr-harness/0.1"
}

func (p *Provider) integrationID() string {
	if p.IntegrationID != "" {
		return p.IntegrationID
	}
	return defaultIntegrationID
}

// ---------- Device-code OAuth flow ----------

// DeviceCode is the response from POST /login/device/code.
type DeviceCode struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// StartDeviceFlow begins a device-code OAuth flow against GitHub.
// Returns the codes; the caller displays UserCode + VerificationURI
// to the user and then polls FinishDeviceFlow.
func StartDeviceFlow(ctx context.Context, clientID string) (*DeviceCode, error) {
	if clientID == "" {
		clientID = DefaultClientID
	}
	v := url.Values{}
	v.Set("client_id", clientID)
	v.Set("scope", "read:user")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, deviceCodeURL,
		strings.NewReader(v.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("device code: HTTP %d: %s", resp.StatusCode, body)
	}
	var dc DeviceCode
	if err := json.NewDecoder(resp.Body).Decode(&dc); err != nil {
		return nil, err
	}
	if dc.Interval == 0 {
		dc.Interval = 5
	}
	return &dc, nil
}

// FinishDeviceFlow polls until the user completes the GitHub
// authorization. Returns the GitHub access token.
func FinishDeviceFlow(ctx context.Context, clientID string, dc *DeviceCode) (string, error) {
	if clientID == "" {
		clientID = DefaultClientID
	}
	deadline := time.Now().Add(time.Duration(dc.ExpiresIn) * time.Second)
	if dc.ExpiresIn == 0 {
		deadline = time.Now().Add(15 * time.Minute)
	}
	interval := time.Duration(dc.Interval) * time.Second
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(interval):
		}
		v := url.Values{}
		v.Set("client_id", clientID)
		v.Set("device_code", dc.DeviceCode)
		v.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, accessTokenURL, strings.NewReader(v.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Accept", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			continue
		}
		var parsed struct {
			AccessToken      string `json:"access_token"`
			Error            string `json:"error"`
			ErrorDescription string `json:"error_description"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&parsed)
		_ = resp.Body.Close()
		if parsed.AccessToken != "" {
			return parsed.AccessToken, nil
		}
		switch parsed.Error {
		case "authorization_pending":
			continue
		case "slow_down":
			interval += 5 * time.Second
			continue
		case "":
			continue
		default:
			return "", fmt.Errorf("device flow: %s: %s", parsed.Error, parsed.ErrorDescription)
		}
	}
	return "", errors.New("device flow: expired without authorization")
}

// Errors.
var (
	ErrNoGHToken = errors.New("copilot: GitHub access token not set; run device-flow auth first")
)
