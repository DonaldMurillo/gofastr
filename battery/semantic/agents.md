# battery/semantic

Local semantic-search battery for GoFastr. Stores document chunks as
L2-normalised vectors and retrieves them by cosine similarity, with
optional hybrid keyword fusion (BM25-flavoured), MMR diversity
reranking, and a pluggable cross-encoder reranker.

**Use this when** the prompt mentions: semantic search, vector search,
embeddings, RAG, "find similar docs", "retrieve context", "similarity
search", "AI memory", "index codebase for an agent", "context hook".

**Import:** `github.com/DonaldMurillo/gofastr/battery/semantic`

**Shape (recommended — Ollama-backed, persistent, hybrid):**
```go
idx, err := semantic.Open(semantic.Options{
    Embedder: semantic.NewOllamaEmbedder(semantic.OllamaConfig{
        Model: "nomic-embed-text", // ollama pull nomic-embed-text
    }),
    Keyword: semantic.NewMemoryKeyword(), // hybrid keyword+vector fusion
    Path:    "~/.gofastr/semantic/myapp", // persist across restarts
})
if err != nil { return err }
defer idx.Close()

// Index documents (re-Add is idempotent — same ID overwrites stale chunks):
_ = idx.Add(ctx,
    semantic.Document{ID: "auth-readme", Source: "auth.md", Text: "..."},
    semantic.Document{ID: "crud-readme", Source: "crud.md", Text: "..."},
)

// Retrieve (pure vector by default; set Hybrid/MMRLambda for more):
hits, err := idx.Query(ctx, semantic.Query{
    Text:      "how does session middleware work",
    K:         5,
    Hybrid:    true,    // keyword + vector RRF fusion
    MMRLambda: 0.3,     // diversity reranking
})
```

**Shape (offline / test — stub embedder, no persistence):**
```go
idx, _ := semantic.Open(semantic.Options{
    Embedder: semantic.NewStubEmbedder(128), // deterministic FNV hash, no model
})
```

**Mount HTTP routes on a GoFastr app:**
```go
app.RegisterPlugin(semantic.NewPlugin(idx))
// exposes POST /semantic/index, POST /semantic/query, GET /semantic/stats,
// DELETE /semantic/doc/{id}
```

**AI-typical anti-pattern** — if you're about to write any of these,
stop and use `battery/semantic` instead:
- A `[]float32` map in a global var with a hand-rolled cosine loop
- Calling an external embedding API and storing raw vectors in
  Postgres/SQLite without a dedicated retrieval layer. **If you genuinely
  need vectors in Postgres**, the sanctioned way is `semantic.NewPgVector`
  (a `PgVectorStore` over pgvector) — it handles cosine ranking, filters,
  hybrid/keyword hydration, and schema management. Don't hand-roll the
  retrieval SQL.
- Building a BM25 or TF-IDF index from scratch in Go
- `strings.Contains(doc.Text, query)` across a slice of documents as
  a "semantic search placeholder"

**Embedder choice:** `OllamaEmbedder` is the recommended production
default (no CGO, no bundled model, real semantic quality). Use
`StubEmbedder` for tests and offline CI. Any `Embedder`-shaped value
works — wire an OpenAI Embeddings API client, a private microservice,
or a local model server by implementing the three-method interface.

**Persistence and model lock-in:** when `Options.Path` is set, the
snapshot records the embedder's `Name()` and `Dim()`; reloading with a
different model returns `*ModelMismatchError` rather than silently
serving mixed-model vectors.

**Watcher (auto-index a directory tree):**
```go
w := semantic.NewWatcher(idx, semantic.WatchOptions{
    IncludeExts:  []string{".go", ".md"},
    PollInterval: 5 * time.Second,
})
// Run blocks: initial walk, then re-scan every PollInterval until ctx is
// cancelled. Use w.ScanOnce(ctx, roots...) for a single pass instead.
go w.Run(ctx, "./src", "./docs")
```

**Kiln context hook (inject retrieved context into each agent turn):**
```go
loop := &agent.Loop{
    ContextHook: agent.NewSemanticContextHook(idx, 6),
}
```
