package semantic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

// OllamaEmbedder is an Embedder over an Ollama-compatible /api/embed endpoint.
type OllamaEmbedder struct {
	baseURL string
	model   string
	dim     atomic.Int64
	client  *http.Client
}

// OllamaConfig configures an [OllamaEmbedder].
type OllamaConfig struct {
	// BaseURL is the server root. Defaults to "http://localhost:11434".
	BaseURL string
	// Model is the embedding model name. Defaults to "nomic-embed-text".
	Model string
	// Dim sets the embedding dimension so [FlatStore] can size itself on open.
	// If zero, the first successful [Embed] response determines the dimension.
	Dim int
	// Timeout is the per-request HTTP timeout. Defaults to 30s.
	Timeout time.Duration
	// Client is an optional pre-configured http.Client; nil builds one with
	// Timeout.
	Client *http.Client
}

// NewOllamaEmbedder constructs an Ollama-backed embedder; it makes no network
// call until Embed.
func NewOllamaEmbedder(cfg OllamaConfig) *OllamaEmbedder {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "http://localhost:11434"
	}
	if cfg.Model == "" {
		cfg.Model = "nomic-embed-text"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	client := cfg.Client
	if client == nil {
		client = &http.Client{Timeout: cfg.Timeout}
	}
	e := &OllamaEmbedder{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		model:   cfg.Model,
		client:  client,
	}
	e.dim.Store(int64(cfg.Dim))
	return e
}

// Name returns "ollama:<model>", the fingerprint persisted indexes check.
func (e *OllamaEmbedder) Name() string { return "ollama:" + e.model }

// Dim returns the embedding dimension, or 0 before it is known.
func (e *OllamaEmbedder) Dim() int { return int(e.dim.Load()) }

type ollamaRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type ollamaResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
	// Single-prompt API fallback (older Ollama versions):
	Embedding []float32 `json:"embedding"`
	Error     string    `json:"error,omitempty"`
}

// Embed calls /api/embed with the whole batch, falling back to the
// single-prompt API of older servers.
func (e *OllamaEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	body, err := json.Marshal(ollamaRequest{Model: e.model, Input: texts})
	if err != nil {
		return nil, fmt.Errorf("semantic: marshal ollama request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("semantic: build ollama request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("semantic: ollama request: %w (is `ollama serve` running at %s?)", err, e.baseURL)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, (10<<20)+1))
	if err != nil {
		return nil, fmt.Errorf("semantic: read ollama response: %w", err)
	}
	if len(raw) > 10<<20 {
		return nil, errors.New("semantic: ollama response exceeds 10 MB")
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("semantic: ollama returned %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var out ollamaResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("semantic: decode ollama response: %w (body=%q)", err, raw)
	}
	if out.Error != "" {
		return nil, errors.New("semantic: ollama: " + out.Error)
	}

	embeddings := out.Embeddings
	if len(embeddings) == 0 && len(out.Embedding) > 0 && len(texts) == 1 {
		embeddings = [][]float32{out.Embedding}
	}
	if len(embeddings) != len(texts) {
		return nil, fmt.Errorf("semantic: ollama returned %d embeddings for %d inputs", len(embeddings), len(texts))
	}

	dim := len(embeddings[0])
	if dim == 0 {
		return nil, errors.New("semantic: ollama returned an empty embedding")
	}
	for _, vec := range embeddings {
		if len(vec) != dim {
			return nil, errors.New("semantic: ollama returned inconsistent embedding dimensions")
		}
	}
	cached := e.Dim()
	if cached != 0 && cached != dim {
		return nil, fmt.Errorf("semantic: ollama dimension %d does not match %d", dim, cached)
	}
	if !e.dim.CompareAndSwap(0, int64(dim)) && e.Dim() != dim {
		return nil, fmt.Errorf("semantic: ollama dimension changed to %d", dim)
	}
	for i := range embeddings {
		normalize(embeddings[i])
	}

	return embeddings, nil
}

// Probe makes a tiny embedding call to detect Dim and confirm the server is
// reachable.
func (e *OllamaEmbedder) Probe(ctx context.Context) error {
	vecs, err := e.Embed(ctx, []string{"probe"})
	if err != nil {
		return err
	}
	if len(vecs) != 1 || len(vecs[0]) == 0 {
		return errors.New("semantic: ollama probe returned no embedding")
	}
	var sumsq float64
	for _, x := range vecs[0] {
		sumsq += float64(x) * float64(x)
	}
	if math.Abs(sumsq-1.0) > 0.01 {
		return fmt.Errorf("semantic: ollama embedding not L2-normalised after probe (sumsq=%f)", sumsq)
	}
	return nil
}

var _ Embedder = (*OllamaEmbedder)(nil)
