// Package main is a minimal demonstration of the battery/embed package.
//
// What it does:
//
//  1. Opens an in-process [embed.Index] with the dependency-free stub
//     embedder and the in-process [embed.MemoryKeyword] backend.
//  2. Indexes a handful of seed documents so a fresh run is already
//     queryable.
//  3. Registers [embed.Plugin] on a framework.App so the standard
//     /embed/index, /embed/query, /embed/stats, and /embed/doc/{id}
//     routes are auto-mounted.
//
// Run with:
//
//	go run ./examples/embed-demo
//
// Then exercise the API:
//
//	curl 'http://localhost:8086/embed/stats'
//	curl -X POST 'http://localhost:8086/embed/query' \
//	    -H 'content-type: application/json' \
//	    -d '{"text":"cache battery","k":3,"hybrid":true}'
//	curl -X POST 'http://localhost:8086/embed/index' \
//	    -H 'content-type: application/json' \
//	    -d '{"documents":[{"id":"new","text":"my new doc"}]}'
//
// The example uses the stub embedder, so retrieval is keyword-strong
// and semantic-weak. Swap in the ONNX embedder (M1.5) for real
// semantic similarity.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/DonaldMurillo/gofastr/battery/embed"
	"github.com/DonaldMurillo/gofastr/framework"
)

func main() {
	app := framework.NewApp(
		framework.WithConfig(framework.AppConfig{Name: "embed-demo"}),
	)

	idx, err := embed.Open(embed.Options{
		Embedder: embed.NewStubEmbedder(128),
		Keyword:  embed.NewMemoryKeyword(),
	})
	if err != nil {
		log.Fatalf("embed.Open: %v", err)
	}

	seed(idx)

	app.RegisterPlugin(embed.NewPlugin(idx))
	if err := app.InitPlugins(); err != nil {
		log.Fatalf("InitPlugins: %v", err)
	}

	addr := ":8086"
	fmt.Printf("embed-demo listening on http://localhost%s\n", addr)
	fmt.Printf("try: curl 'http://localhost%s/embed/stats'\n", addr)
	if err := http.ListenAndServe(addr, app.Router); err != nil {
		log.Fatal(err)
	}
}

// seed populates the index with a few documents so a fresh run has
// something to retrieve. In a real app these would be loaded from
// disk via [embed.Watcher] or pushed in via POST /embed/index.
func seed(idx embed.Index) {
	ctx := context.Background()
	docs := []embed.Document{
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
			ID:     "battery-embed",
			Source: "battery/embed/doc.go",
			Text:   "The embed battery adds local semantic search: ONNX-backed embeddings, in-memory cosine, hybrid keyword fusion, MMR diversity, and a Kiln agent context hook.",
		},
	}
	if err := idx.Add(ctx, docs...); err != nil {
		log.Fatalf("seed: %v", err)
	}
}
