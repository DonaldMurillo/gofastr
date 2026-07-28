package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/semantic"
	"github.com/DonaldMurillo/gofastr/framework"
)

// TestEmbedDemoSmoke wires the demo's plugin onto a fresh App + Router
// and verifies the /semantic/* routes actually answer.
func TestEmbedDemoSmoke(t *testing.T) {
	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "semantic-demo-test"}))
	idx, err := semantic.Open(semantic.Options{
		Embedder: semantic.NewStubEmbedder(64),
		Keyword:  semantic.NewMemoryKeyword(),
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	seed(idx)
	app.RegisterPlugin(semantic.NewPlugin(idx))
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}

	srv := httptest.NewServer(app.Router())
	defer srv.Close()

	// /semantic/* routes now require auth (security hardening).
	authed := func(req *http.Request) *http.Request {
		req.Header.Set("Authorization", "Bearer test")
		return req
	}
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/semantic/stats", nil)
	resp, err := http.DefaultClient.Do(authed(req))
	if err != nil {
		t.Fatalf("GET /semantic/stats: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var stats semantic.Stats
	json.NewDecoder(resp.Body).Decode(&stats)
	if stats.Docs < 4 {
		t.Fatalf("stats.Docs = %d, want >= 4", stats.Docs)
	}

	body := strings.NewReader(`{"text":"cache battery","k":3,"hybrid":true}`)
	req, _ = http.NewRequest(http.MethodPost, srv.URL+"/semantic/query", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(authed(req))
	if err != nil {
		t.Fatalf("POST /semantic/query: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		buf := new(bytes.Buffer)
		buf.ReadFrom(resp.Body)
		t.Fatalf("query status = %d body=%s", resp.StatusCode, buf)
	}
	var qr struct{ Hits []semantic.Hit }
	json.NewDecoder(resp.Body).Decode(&qr)
	if len(qr.Hits) == 0 {
		t.Fatalf("no hits")
	}
	if qr.Hits[0].Chunk.DocID != "battery-cache" {
		t.Fatalf("top hit doc=%q, want battery-cache (hits=%+v)", qr.Hits[0].Chunk.DocID, qr.Hits)
	}
}
