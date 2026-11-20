package openrouter

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestSweepModelsCatalog(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		fmt.Fprint(w, `{"data":[{"id":"a/b","name":"B","context_length":1000,"pricing":{"prompt":"0.000003","completion":"bad"},"top_provider":{"max_completion_tokens":50}}]}`)
	}))
	defer srv.Close()
	p := &Provider{APIKey: "k", BaseURL: srv.URL}
	ms, err := p.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 || ms[0].Pricing.InputPerMTok < 2.999 || ms[0].Pricing.InputPerMTok > 3.001 || ms[0].Pricing.OutputPerMTok != 0 || ms[0].MaxOutput != 50 {
		t.Fatalf("models = %+v, want per-token price scaled to per-MTok and an unparsable price as 0", ms)
	}
	ms[0].ID = "mutated"
	again, err := p.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	again[0].ID = "mutated"
	again, _ = p.Models(context.Background())
	if hits.Load() != 1 || again[0].ID != "a/b" {
		t.Fatalf("hits = %d, id = %q: want the cached catalog, unaffected by a caller's edit", hits.Load(), again[0].ID)
	}
}

func TestSweepModelsRefusesFoldedKeys(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[],"Data":[{"id":"smuggled"}]}`)
	}))
	defer srv.Close()
	p := &Provider{APIKey: "k", BaseURL: srv.URL}
	if ms, err := p.Models(context.Background()); err == nil {
		t.Fatalf("Models accepted a case-folded duplicate key, got %+v", ms)
	}
}

func TestSweepModelsErrorPreviewIsScrubbed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("no\x1b[2K\rFORGED"))
	}))
	defer srv.Close()
	p := &Provider{APIKey: "k", BaseURL: srv.URL}
	_, err := p.Models(context.Background())
	if err == nil || strings.ContainsAny(err.Error(), "\x1b\r") || !strings.Contains(err.Error(), "HTTP 403") {
		t.Fatalf("err = %v, want status kept and control bytes stripped", err)
	}
}
