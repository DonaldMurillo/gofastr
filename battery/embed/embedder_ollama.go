package embed

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

// OllamaEmbedder is an [Embedder] that calls a locally running Ollama
// (or any compatible) server's /api/embed endpoint. Ollama itself,
// LM Studio, and llama.cpp's server all expose this contract.
//
// This is the recommended swap from [StubEmbedder] for production use:
//
//	idx, _ := embed.Open(embed.Options{
//	    Embedder: embed.NewOllamaEmbedder(embed.OllamaConfig{
//	        Model: "nomic-embed-text",
//	    }),
//	})
//
// Pre-flight: run `ollama serve` (it auto-starts on most installs) and
// `ollama pull nomic-embed-text`. The default model is 768-dim and is
// the standard recommendation for local semantic search.
//
// Failure modes:
//
//   - Server unreachable → [Embed] returns the underlying network
//     error. Callers should not blindly retry — a missing daemon is a
//     configuration problem, not a transient one.
//   - Model not pulled → Ollama responds 404 with a clear message; we
//     surface it verbatim so users see "pull <model> first".
//   - Dim mismatch with the configured [FlatStore] is detected later
//     at [Store.Add] time via [errVecDim].
type OllamaEmbedder struct {
	baseURL string
	model   string
	// dim is the embedding dimension, lazily cached on the first successful
	// Embed when not set at construction. Atomic because Embed (writer) and
	// Dim (reader) may run on different goroutines concurrently.
	dim    atomic.Int64
	client *http.Client
}

// OllamaConfig configures an [OllamaEmbedder].
type OllamaConfig struct {
	// BaseURL is the Ollama server root. Defaults to
	// "http://localhost:11434". Trailing slashes are trimmed.
	BaseURL string
	// Model is the embedding model name (e.g. "nomic-embed-text",
	// "mxbai-embed-large", "all-minilm"). Defaults to
	// "nomic-embed-text". Whatever you pick must be a model the server
	// has pulled.
	Model string
	// Dim is the embedding dimension. If 0, it is detected on the
	// first call by inspecting the response. Setting it explicitly
	// skips a probe at construction time and lets [FlatStore] pre-size.
	Dim int
	// Timeout is the per-request HTTP timeout. Defaults to 30s.
	Timeout time.Duration
	// Client is an optional pre-configured http.Client (for tests or
	// custom transport). When nil, a fresh client with the timeout is
	// constructed.
	Client *http.Client
}

// NewOllamaEmbedder constructs an Ollama-backed embedder. It does not
// make any network calls until [Embed] is invoked — failures during
// boot are deferred to first use so misconfigurations can be retried
// without restarting the app.
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

// Name returns "ollama:<model>" — the snapshot fingerprint baked into
// persisted indexes uses this string, so changing models triggers a
// loud [ModelMismatchError] on reload.
func (e *OllamaEmbedder) Name() string { return "ollama:" + e.model }

// Dim returns the embedding dimension. If the dimension was not set
// in the config and no embedding has been produced yet, Dim returns
// 0; [Open] should be passed a config with Dim set when the store is
// initialised separately.
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

// Embed calls /api/embed with the entire batch. Ollama performs the
// batching server-side, so we hand the slice over whole instead of
// looping client-side.
func (e *OllamaEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	body, err := json.Marshal(ollamaRequest{Model: e.model, Input: texts})
	if err != nil {
		return nil, fmt.Errorf("embed: marshal ollama request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("embed: build ollama request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embed: ollama request: %w (is `ollama serve` running at %s?)", err, e.baseURL)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20)) // 10 MB max
	if err != nil {
		return nil, fmt.Errorf("embed: read ollama response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("embed: ollama returned %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var out ollamaResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("embed: decode ollama response: %w (body=%q)", err, raw)
	}
	if out.Error != "" {
		return nil, errors.New("embed: ollama: " + out.Error)
	}

	embeddings := out.Embeddings
	// Older Ollama servers return /api/embeddings shape (single
	// embedding per request). If we batched into a server that does
	// not understand the batch API, fall back gracefully.
	if len(embeddings) == 0 && len(out.Embedding) > 0 && len(texts) == 1 {
		embeddings = [][]float32{out.Embedding}
	}
	if len(embeddings) != len(texts) {
		return nil, fmt.Errorf("embed: ollama returned %d embeddings for %d inputs", len(embeddings), len(texts))
	}

	for i := range embeddings {
		normalize(embeddings[i])
	}

	// Cache the dimension on first successful call. CompareAndSwap so a
	// concurrent caller can't race two writers to a torn value.
	if len(embeddings[0]) > 0 {
		e.dim.CompareAndSwap(0, int64(len(embeddings[0])))
	}
	return embeddings, nil
}

// Probe makes a tiny embedding call to detect Dim and confirm the
// server is reachable. Useful at app boot so dim-mismatch errors
// surface before the first user query rather than after.
func (e *OllamaEmbedder) Probe(ctx context.Context) error {
	vecs, err := e.Embed(ctx, []string{"probe"})
	if err != nil {
		return err
	}
	if len(vecs) != 1 || len(vecs[0]) == 0 {
		return errors.New("embed: ollama probe returned no embedding")
	}
	// Sanity check normalisation worked.
	var sumsq float64
	for _, x := range vecs[0] {
		sumsq += float64(x) * float64(x)
	}
	if math.Abs(sumsq-1.0) > 0.01 {
		return fmt.Errorf("embed: ollama embedding not L2-normalised after probe (sumsq=%f)", sumsq)
	}
	return nil
}

var _ Embedder = (*OllamaEmbedder)(nil)
