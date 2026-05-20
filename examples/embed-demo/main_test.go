package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/embed"
	"github.com/DonaldMurillo/gofastr/framework"
)

// TestEmbedDemoSmoke wires the demo's plugin onto a fresh App + Router
// and verifies the /embed/* routes actually answer.
func TestEmbedDemoSmoke(t *testing.T) {
	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "embed-demo-test"}))
	idx, err := embed.Open(embed.Options{
		Embedder: embed.NewStubEmbedder(64),
		Keyword:  embed.NewMemoryKeyword(),
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	seed(idx)
	app.RegisterPlugin(embed.NewPlugin(idx))
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}

	srv := httptest.NewServer(app.Router())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/embed/stats")
	if err != nil {
		t.Fatalf("GET /embed/stats: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var stats embed.Stats
	json.NewDecoder(resp.Body).Decode(&stats)
	if stats.Docs < 4 {
		t.Fatalf("stats.Docs = %d, want >= 4", stats.Docs)
	}

	body := strings.NewReader(`{"text":"cache battery","k":3,"hybrid":true}`)
	resp, err = http.Post(srv.URL+"/embed/query", "application/json", body)
	if err != nil {
		t.Fatalf("POST /embed/query: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		buf := new(bytes.Buffer)
		buf.ReadFrom(resp.Body)
		t.Fatalf("query status = %d body=%s", resp.StatusCode, buf)
	}
	var qr struct{ Hits []embed.Hit }
	json.NewDecoder(resp.Body).Decode(&qr)
	if len(qr.Hits) == 0 {
		t.Fatalf("no hits")
	}
	if qr.Hits[0].Chunk.DocID != "battery-cache" {
		t.Fatalf("top hit doc=%q, want battery-cache (hits=%+v)", qr.Hits[0].Chunk.DocID, qr.Hits)
	}
}
