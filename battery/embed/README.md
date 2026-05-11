# battery/embed

Local semantic-search battery for GoFastr.

```go
idx, _ := embed.Open(embed.Options{
    // Real semantic — recommended once Ollama is running locally:
    Embedder: embed.NewOllamaEmbedder(embed.OllamaConfig{
        Model: "nomic-embed-text",            // ollama pull nomic-embed-text
    }),
    // Or the dependency-free deterministic stub for tests/offline dev:
    // Embedder: embed.NewStubEmbedder(128),
    Keyword:  embed.NewMemoryKeyword(),       // optional, enables hybrid
    Path:     "~/.gofastr/embed/myapp",       // optional, enables persistence
})
defer idx.Close()

idx.Add(ctx, embed.Document{ID: "auth", Source: "auth.go",
    Text: "Auth middleware verifies sessions and JWTs."})

hits, _ := idx.Query(ctx, embed.Query{
    Text:      "how does session middleware work",
    K:         5,
    Hybrid:    true,    // keyword + vector RRF fusion
    MMRLambda: 0.3,     // diversity reranking
})
```

## What's in the box

| File | What it does |
| --- | --- |
| `embed.go` | Public types: `Document`, `Chunk`, `Hit`, `Query`, `Filter`, `Stats`. The `Embedder`, `Chunker`, `Store`, `KeywordBackend`, `Reranker` interfaces. `Open(Options) Index`. |
| `index.go` | The default `Index` implementation. Orchestrates chunker → embedder → store → retrieval pipeline. WAL + snapshot lifecycle. |
| `store_flat.go` | `FlatStore`: in-memory `[]Chunk`, brute-force cosine, doc-scoped removal. Targets ~100k chunks at 384 dims (~150MB). |
| `chunker.go` | `FixedWindow`: language-agnostic rune-window chunker with overlap. Default. |
| `chunker_lang.go` | `LangAware`: Go AST-aware + Markdown heading-aware; falls back to `FixedWindow` per-section when chunks exceed `MaxRunes`. |
| `stub_embedder.go` | `StubEmbedder`: deterministic FNV-hashed bag-of-words. Test and offline-dev only — **not** a real model. |
| `embedder_ollama.go` | `OllamaEmbedder`: HTTP client against Ollama-compatible `/api/embed`. Real semantic embeddings, no CGO, no bundled model. Recommended production default. |
| `hybrid.go` | `MemoryKeyword`: in-process BM25-flavoured keyword backend. `fuseRRF` reciprocal-rank fusion. |
| `hybrid_search.go` | `WrapSearchBackend`: adapter from `battery/search.Backend` into `KeywordBackend`. |
| `mmr.go` | Maximal Marginal Relevance reranker. Run after candidate generation, before final top-K. |
| `persist.go` | Gob snapshot (atomic rename) + append-only WAL with replay. Model-fingerprint guard. |
| `watcher.go` | `Watcher`: polling filesystem watcher with include-exts, exclude-dirs, replace-on-mtime, delete-on-vanish. |
| `plugin.go` | `framework.Plugin` adapter that auto-mounts `/embed/*` routes. |
| `routes.go` | Stdlib `http.Handler` exposing `POST /index`, `POST /query`, `GET /stats`, `DELETE /doc/{id}`. |

## Retrieval pipeline

```text
Query.Text
  │
  ├── Embedder.Embed(qv)
  ├── Store.Candidates(qv, Filter, 4×K)          → vector hits
  └── KeywordBackend.Search(text, 4×K)           → keyword hits   (if Hybrid)
                │
                └── fuseRRF                       → fused hits
                                    │
                                    └── mmr      → diverse top K (if MMRLambda > 0)
                                                        │
                                                        └── Reranker.Rerank (if Rerank)
                                                                │
                                                                └── strip Vec → caller
```

The pipeline is opt-in: a default `Query{Text: "x"}` runs pure vector retrieval. Setting `Hybrid` enables fusion; `MMRLambda` enables diversity; `Rerank` requires `Options.Reranker` to be set (otherwise the call errors — silent quality regressions are not allowed).

## Persistence

When `Options.Path` is set, every mutation appends to `<path>/store.wal` and reads back on `Open`. Every `Options.SnapshotEvery` mutations (default 1000) or on `Index.Snapshot()`, the full state is written atomically to `<path>/store.snap` and the WAL is truncated.

The snapshot header records the embedder's `Name()` and `Dim()`. A mismatch on load returns `*ModelMismatchError` and refuses to load — mixing vectors from different models silently destroys retrieval quality.

## Watcher

`embed.NewWatcher(idx, embed.WatchOptions{...})` walks roots, applies include-exts (`.go`, `.md`, …), excludes well-known dirs (`.git`, `node_modules`, …), and polls every 2s by default. Replace-by-doc semantics in `Index.Add` mean a file edit re-chunks cleanly without leaving stale chunks behind.

## HTTP surface

| Method | Path | Body | Returns |
| --- | --- | --- | --- |
| POST | `/embed/index` | `{"documents":[…]}` | 202 + `{"added": N}` |
| POST | `/embed/query` | `Query` | `{"hits":[…]}` |
| GET  | `/embed/stats` | — | `Stats` |
| DELETE | `/embed/doc/{id}` | — | 204 |

Mount via the plugin:

```go
app.RegisterPlugin(embed.NewPlugin(idx))
app.InitPlugins()
```

Or wire the raw handler onto any router:

```go
mux.Handle("/embed/", http.StripPrefix("/embed", embed.Handler(idx)))
```

## CLI

```bash
gofastr embed index .                          # one-shot index of cwd
gofastr embed watch ./src ./docs               # poll-watch until SIGINT
gofastr embed query "auth middleware" -k 5 --hybrid
gofastr embed stats
gofastr embed clear                            # wipe local snapshot
```

When `GOFASTR_URL` is set, `query` and `stats` hit that server's `/embed/*` routes instead of opening a local index.

## Kiln integration

`kiln.Loop.ContextHook` is a per-turn callback that prepends retrieved context to the system prompt. Wire it with the helper:

```go
loop := &agent.Loop{
    Provider:    provider,
    Tools:       tools,
    ContextHook: agent.NewEmbedContextHook(idx, 6),
}
```

Each user turn re-queries the index against the latest user message and injects the top 6 chunks as `# Project context` ahead of the framework's built-in prompt.

## Roadmap

| Milestone | Status |
| --- | --- |
| M1 — package skeleton, types, stub embedder, flat store, chunker | ✅ |
| M1.5 — real semantic embedder (Ollama-compatible HTTP) | ✅ |
| M1.6 — in-process ONNX embedder behind CGO build tag | ⏳ |
| M2 — gob snapshot + WAL, plugin + HTTP routes | ✅ |
| M3 — hybrid retrieval, filters, MMR, rerank hook | ✅ |
| M4 — polling watcher, `gofastr embed` CLI | ✅ |
| M5 — Kiln context hook, lang-aware chunker, example app, benches, docs | ✅ |

**Why Ollama over ONNX-direct for M1.5:** the original questionnaire chose "pure-Go ONNX runtime", but in practice that requires CGO (`onnxruntime_go`) + a platform-specific shared library at runtime + a WordPiece tokenizer + a ~23 MB bundled model — all to replace one HTTP call. The Ollama path delivers real semantic embeddings today with zero CGO, zero binary bloat, and a one-line swap from the stub. The same `Embedder` interface means a future in-process ONNX implementation (M1.6, build-tag-gated) drops in additively without touching the rest of the package.
