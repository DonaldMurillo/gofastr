// Package main is a minimal demonstration of the battery/semantic package.
//
// What it does:
//
//  1. Opens an in-process [semantic.Index] with the dependency-free stub
//     embedder and the in-process [semantic.MemoryKeyword] backend.
//  2. Indexes a handful of seed documents so a fresh run is already
//     queryable.
//  3. Registers [semantic.Plugin] on a framework.App so the standard
//     /semantic/index, /semantic/query, /semantic/stats, and /semantic/doc/{id}
//     routes are auto-mounted.
//
// Run with:
//
//	go run ./examples/semantic-demo
//
// Then exercise the API:
//
//	curl 'http://localhost:8086/semantic/stats'
//	curl -X POST 'http://localhost:8086/semantic/query' \
//	    -H 'content-type: application/json' \
//	    -d '{"text":"cache battery","k":3,"hybrid":true}'
//	curl -X POST 'http://localhost:8086/semantic/index' \
//	    -H 'content-type: application/json' \
//	    -d '{"documents":[{"id":"new","text":"my new doc"}]}'
//
// The example uses the stub embedder, so retrieval is keyword-strong
// and semantic-weak. Swap in [semantic.NewOllamaEmbedder] (with a
// local Ollama server) for real semantic similarity.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/DonaldMurillo/gofastr/battery/semantic"
	"github.com/DonaldMurillo/gofastr/framework"
)

func main() {
	app := framework.NewApp(
		framework.WithConfig(framework.AppConfig{Name: "semantic-demo"}),
	)

	idx, err := semantic.Open(semantic.Options{
		Embedder: semantic.NewStubEmbedder(128),
		Keyword:  semantic.NewMemoryKeyword(),
	})
	if err != nil {
		log.Fatalf("semantic.Open: %v", err)
	}

	seed(idx)

	app.RegisterPlugin(semantic.NewPlugin(idx))
	if err := app.InitPlugins(); err != nil {
		log.Fatalf("InitPlugins: %v", err)
	}

	addr := ":8086"
	fmt.Printf("semantic-demo listening on http://localhost%s\n", addr)
	fmt.Printf("try: curl 'http://localhost%s/semantic/stats'\n", addr)
	if err := http.ListenAndServe(addr, app.Router()); err != nil {
		log.Fatal(err)
	}
}

// seed populates the index with a few documents so a fresh run has
// something to retrieve. In a real app these would be loaded from
// disk via [semantic.Watcher] or pushed in via POST /semantic/index.
func seed(idx semantic.Index) {
	ctx := context.Background()
	docs := []semantic.Document{
		{
			ID:     "battery-cache",
			Source: "battery/cache/doc.go",
			Text:   "The cache battery provides pluggable cache implementations: in-memory with TTL and a Redis backend, both fronted by the same Cache interface.",
		},
		{
			ID:     "battery-auth",
			Source: "battery/auth/doc.go",
			Text:   "The auth battery offers session-based and JWT-based authentication, password hashing utilities, and middleware that protects routes by extracting and validating credentials.",
		},
		{
			ID:     "battery-search",
			Source: "battery/search/doc.go",
			Text:   "The search battery exposes a Backend interface with in-memory keyword search; suitable for tests and small examples.",
		},
		{
			ID:     "battery-semantic",
			Source: "battery/semantic/doc.go",
			Text:   "The semantic battery adds local vector search: Ollama-compatible HTTP embeddings, in-memory cosine, hybrid keyword fusion, MMR diversity, and a Kiln agent context hook.",
		},
	}
	if err := idx.Add(ctx, docs...); err != nil {
		log.Fatalf("seed: %v", err)
	}
}
