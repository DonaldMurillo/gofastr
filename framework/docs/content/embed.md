# Embed — local semantic search

`battery/embed` adds vector-based semantic retrieval to any GoFastr app. It is positioned next to `battery/search` (keyword) and `battery/cache`: one Go API, one HTTP surface, one CLI subcommand, one Kiln integration hook.

This doc covers the architecture, persistence, watcher, hybrid pipeline, and the agent integration. For the package-level cheatsheet and file map, see [`battery/embed/README.md`](../battery/embed/README.md).

## Why this lives in the framework

GoFastr's bet is that AI-era apps want retrieval as a primitive, not as a service. Three properties follow:

- **In-process by default.** No vector DB, no embedding service. The default index runs in the same process as your routes, hooks, and Kiln agent.
- **Pure-Go core.** Brute-force cosine, gob snapshot, polling watcher — no third-party deps in the core path. The only optional CGO is the ONNX runtime that drives the default embedder in M1.5.
- **Composable surfaces.** Go API for in-app use; HTTP routes for cross-process/cross-host; CLI for ops; Kiln hook for agent context. The same index serves all four.

## Architecture

```text
            ┌─────────────────┐
            │   Document      │   doc.ID, doc.Text, doc.Metadata
            └────────┬────────┘
                     │
              Chunker.Chunk()           (FixedWindow | LangAware | user)
                     │
                     ▼
            ┌─────────────────┐
            │     Chunk       │   chunk.ID, chunk.Vec
            └────────┬────────┘
                     │
              Embedder.Embed()          (StubEmbedder | ONNX | user)
                     │
                     ▼
            ┌─────────────────┐
            │   Store.Add     │   FlatStore (in-memory, brute-force cosine)
            └─────────────────┘

            ┌─────────────────┐         ┌──────────────────┐
            │  Embedder.Embed │◄────────│   Query.Text     │
            └────────┬────────┘         └──────────────────┘
                     │
                     ▼
              Store.Candidates ──────────► top-4K vector hits
                     │
                     │   if Hybrid:
                     │   KeywordBackend.Search → top-4K keyword hits
                     │                          │
                     └─────────► fuseRRF ◄──────┘
                                  │
                                  ▼   if MMRLambda > 0:
                                mmr(λ) ──► diverse top-K
                                  │
                                  │   if Rerank:
                                  ▼
                              Reranker.Rerank
                                  │
                                  ▼
                              top-K Hits
```

The pipeline is composed inside `Index.Query`. Every stage is opt-in via the `Query` struct fields; the zero-value query runs the cheapest, simplest path (vector-only top-10).

## Lifecycle and persistence

| Event | Behaviour |
| --- | --- |
| `Open(Options{Path: dir})` | Load `dir/store.snap` if present; replay `dir/store.wal`. |
| `Add(docs)` / `Remove(ids)` | Append WAL entry (fsync per write); apply to in-memory store. |
| `Snapshot()` | Atomic write of `dir/store.snap.tmp` + rename; truncate WAL. |
| `Add` / `Remove` every `SnapshotEvery` writes | Auto-triggers `Snapshot()`. |
| `Close()` | Final `Snapshot()`; close WAL; close store. |

The snapshot's header records the embedder's `Name()` and `Dim()`. Reopening with a different embedder is refused with `*ModelMismatchError` because mixing vectors from different models is silently catastrophic for retrieval quality. To migrate models: drop the snapshot, re-index from source.

## Hybrid retrieval

`Options.Keyword` injects a `KeywordBackend`. Two implementations ship:

- **`MemoryKeyword`** — in-process BM25-flavoured backend. Zero deps. Recommended default.
- **`WrapSearchBackend(b search.Backend)`** — adapter over `battery/search` so an app that already runs Postgres FTS or a Bleve index reuses it.

When `Query.Hybrid=true` and `Options.Keyword != nil`, the index gathers the top-`4K` candidates from vector and keyword separately, fuses them with reciprocal-rank fusion (`k=60`), and feeds the union into MMR/rerank. When `Keyword == nil`, `Hybrid=true` silently degrades to vector-only.

## Diversity (MMR)

`Query.MMRLambda ∈ [0, 1]` runs Maximal Marginal Relevance over the candidate set:

- `λ = 0` — collapse to pure relevance order (no diversity).
- `λ = 0.3` — useful default; trims near-duplicates without sacrificing relevance.
- `λ = 1` — pick the most-diverse-from-already-picked items regardless of query.

MMR needs the candidate vectors; the store keeps them on the chunks until the very end of the pipeline, then strips them before returning.

## Reranking

`Options.Reranker` is the second-stage scorer hook. The package ships **no** built-in reranker. Setting `Query.Rerank=true` without `Options.Reranker` is an error — silent quality loss is not allowed.

A reranker receives the original query text and the candidate hits; it returns a reordered slice with its own scores. Typical wiring is a cross-encoder model behind an HTTP endpoint:

```go
type httpReranker struct{ url string }

func (r httpReranker) Rerank(ctx context.Context, q string, hits []embed.Hit) ([]embed.Hit, error) {
    // marshal {q, [hit.Text]} → POST → unmarshal scored slice → return
}
```

## Watcher

```go
w := embed.NewWatcher(idx, embed.WatchOptions{
    IncludeExts: []string{".go", ".md", ".markdown", ".txt"},
    ExcludeDirs: []string{".git", "node_modules", "dist", ".gofastr", "vendor"},
    PollInterval: 2 * time.Second,
    MaxFileSize:  1 << 20,
})
w.Run(ctx, "./src", "./docs")     // until ctx cancellation
// or
w.ScanOnce(ctx, "./src")          // one-shot
```

`Watcher` is polling-based on purpose: no third-party deps, deterministic behaviour, and the dev-time cost on a tree of a few thousand files is well under the embedding cost of any actual file edit. For very large trees, swap the implementation for fsnotify-backed without changing the public API.

`Index.Add` is replace-by-doc, so a re-emit of the same `Document.ID` cleanly replaces prior chunks. The watcher derives stable doc IDs from path hashes, so renames look like delete + add and bursty edits collapse into the next poll cycle.

## CLI

The `gofastr embed` subcommand opens a per-cwd local snapshot at `~/.gofastr/embed/<sha1(cwd)>/`. Different projects keep separate snapshots automatically.

```bash
gofastr embed index .                                  # one-shot
gofastr embed watch ./src ./docs                       # until SIGINT
gofastr embed query "validate session token" -k 5 --hybrid
gofastr embed query "router middleware" --mmr 0.4
gofastr embed stats
gofastr embed clear
```

When `GOFASTR_URL` is set, `query` and `stats` are dispatched to that running server's `/embed/*` routes. `index`, `watch`, and `clear` are always local — they mutate state and we don't want two writers fighting for the same file.

## HTTP surface

| Method | Path | Body | Status |
| --- | --- | --- | --- |
| POST | `/embed/index` | `{"documents":[{"id","source","text","metadata"}…]}` | 202 |
| POST | `/embed/query` | `Query` (`text`, `k`, `filter`, `hybrid`, `mmr_lambda`, `rerank`) | 200 |
| GET  | `/embed/stats` | — | 200 |
| DELETE | `/embed/doc/{id}` | — | 204 |

Mount the plugin onto a `framework.App`:

```go
app.RegisterPlugin(embed.NewPlugin(idx).WithPrefix("/embed"))
app.InitPlugins()
```

Or wire the bare `http.Handler` onto a `core/router.Router` or stdlib `http.ServeMux`:

```go
mux.Handle("/embed/", http.StripPrefix("/embed", embed.Handler(idx)))
```

## Kiln integration

`kiln.Loop` gained a `ContextHook func(ctx, userText) string`. It is called once per turn with the most recent user message, and its return value is prepended to the provider's system prompt. The helper `agent.NewEmbedContextHook(idx, k)` wraps an `embed.Index`:

```go
loop := &agent.Loop{
    Provider:    provider,
    Tools:       tools,
    ContextHook: agent.NewEmbedContextHook(idx, 6),
}
```

For every user turn, the hook runs a `Hybrid + MMR=0.3` query against the index and injects the top-6 chunks as a `# Project context` block. Retrieval errors degrade silently to an empty preamble so a misbehaving index never blocks the agent loop.

## Local dev: Docker + live tests

The package's default `go test ./...` covers the pipeline with the stub embedder + an `httptest` Ollama mock. To exercise the *real* semantic path (paraphrase clustering, real retrieval on a real corpus, MMR diversity that actually depends on real geometry), use the live-tagged suite:

```bash
make ollama-up        # docker compose up + pull nomic-embed-text on first run
make embed-live       # go test -tags=live -v ./battery/embed/...
make ollama-down      # tear down the container
make ollama-logs      # tail Ollama logs while debugging
```

`docker-compose.yml` ships at the repo root with a single `ollama` service exposing port `11434` and bind-mounting `./.ollama/` (gitignored) so the ~270 MB model file persists between container restarts and stays out of the repo.

The live suite (in `battery/embed/live_test.go`, guarded by `//go:build live`) asserts:

| Test | What it verifies |
| --- | --- |
| `TestLive_OllamaProbe` | Server reachable, model returns embeddings, dim auto-detected. |
| `TestLive_SemanticSimilarity` | Paraphrase pairs cluster; cross-intent pairs don't — the property the stub embedder cannot satisfy. |
| `TestLive_IndexRetrievalParaphrase` | A 6-doc corpus + a paraphrase query surfaces the right doc at rank #1, both vec-only and hybrid. |
| `TestLive_MMRImprovesDiversityOnNearDuplicates` | MMR top-3 on a near-duplicate corpus surfaces ≥2 distinct topics. |
| `TestLive_PersistenceSurvivesProcessRestart` | Snapshot + reopen with a fresh embedder instance preserves retrieval. Model fingerprint must match. |
| `TestLive_WatcherFeedsRealEmbedder` | Polling watcher round-trips files through a real embedder. |

`make embed-live` refuses to run if `http://localhost:11434/api/tags` doesn't respond — it tells you to `make ollama-up` first.

## Scale targets

| Corpus | Vector RAM (384-dim float32) | Brute-force query latency (Apple M-class) |
| --- | --- | --- |
| 10k chunks | ~15 MB | <1 ms |
| 100k chunks | ~150 MB | ~3 ms |
| 1M chunks | ~1.5 GB | ~30 ms (consider int8 quantization or ANN) |

Numbers above are from the `BenchmarkEmbed_*` family in `battery/embed/bench_test.go` (corpus=10000 measured at ~3.3 ms per query on M4 Pro). Persistence costs scale linearly with chunk count; a 100k-chunk snapshot is ~200 MB on disk.

## Embedders

Two are wired:

- **`StubEmbedder`** — deterministic FNV bag-of-words, no deps, low retrieval quality. Fine for tests and offline development. Not a real model.
- **`OllamaEmbedder`** — HTTP client against a locally running Ollama-compatible server. Default `nomic-embed-text` (768-dim) gives real semantic retrieval. Recommended production default.

For anything else (OpenAI Embeddings API, a private microservice, a CGO-bound model), implement the three-method `Embedder` interface and pass it via `Options.Embedder`. The rest of the package is embedder-agnostic.

```go
// Swap from stub to Ollama:
embedder := embed.NewOllamaEmbedder(embed.OllamaConfig{
    BaseURL: "http://localhost:11434",   // default
    Model:   "nomic-embed-text",         // ollama pull nomic-embed-text first
})

// Optional: confirm the server is reachable and warm the dim cache.
if err := embedder.Probe(ctx); err != nil {
    log.Fatalf("ollama unreachable: %v", err)
}
```

## Limitations and follow-ups

- The watcher does not honour `.gitignore` — only an explicit `ExcludeDirs` list. Glob-level ignore parsing is deferred.
- The flat store is the only backend. ANN backends (HNSW, IVF) are intentional non-goals until benchmarks show brute-force losing.
- Multiple named indexes per process are not supported; the design is one index per app, mirroring the `Options.Keyword` and `Options.Path` shape.
