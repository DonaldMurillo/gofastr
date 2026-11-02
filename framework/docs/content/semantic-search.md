# Semantic search

`battery/semantic` adds vector-based semantic retrieval to any GoFastr app. It sits alongside `battery/search` (keyword) and `battery/cache`: one Go API, one set of HTTP routes, one CLI subcommand, one Kiln integration hook.

This doc covers the architecture, persistence, watcher, hybrid pipeline, and the agent integration. For the package-level cheatsheet and file map, see [`battery/semantic/README.md`](../../../battery/semantic/README.md).

## Why this is in-process by default

Retrieval runs in the same process as your app, instead of requiring a separate vector DB or embedding service. A few things follow from that:

- **In-process by default.** No vector DB, no embedding service. The default index runs in the same process as your routes, hooks, and Kiln agent.
- **Pure-Go core.** Brute-force cosine, gob snapshot, polling watcher. No third-party deps in the core path, no CGO. `Open` has no default embedder: pass `NewOllamaEmbedder` (an HTTP client to a local Ollama server) for real retrieval, or `NewStubEmbedder` for tests.
- **Four entry points, one index.** Go API for in-app use; HTTP routes for cross-process/cross-host; CLI for ops; Kiln hook for agent context. The same index serves all four.

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
              Embedder.Embed()          (StubEmbedder | OllamaEmbedder | user)
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
                                  │   if Rerank (sees every candidate
                                  ▼   when MMR is off):
                              Reranker.Rerank
                                  │
                                  ▼
                              top-K Hits
```

The pipeline is composed inside `Index.Query`. Every stage is opt-in via the `Query` struct fields; the zero-value query runs the cheapest, simplest path (vector-only top-10).

## Lifecycle and persistence

| Event | Behaviour |
| --- | --- |
| `Open(Options{Path: dir})` | Take the writer's lock on `dir/lock` (held until `Close`; another writer, in this process or another, gets `ErrIndexLocked`); load `dir/store.snap` if present; replay `dir/store.wal`, recovering a torn final record; checkpoint replayed state before new writes. |
| `Open(Options{Path: dir, ReadOnly: true})` | Load the snapshot and every whole WAL record, beside a writer that holds the lock. Takes no lock and writes nothing: a torn final record is the writer's append in flight and stays, `Close` takes no snapshot, and a missing `dir` is an empty index. `Add`, `Remove` and `Snapshot` return `ErrReadOnly`. The CLI's `query` and `stats` open this way. |
| `Open` with `Options.Keyword` | Rebuild the keyword index from every chunk the store holds, after the snapshot and WAL load. Snapshots carry no keyword state, so without the rebuild the hybrid keyword leg starts empty. `PgVectorStore.Keyword()` reads the table itself and is not rebuilt. |
| `Add(docs)` / `Remove(ids)` | Append WAL entry (fsync per write); apply to in-memory store. An append that fails (a full disk) is cut back off the WAL and the write fails; the next write snapshots first, so a later restart still opens. Replacing a document (remove its chunks, add the new ones) runs as one step and is one WAL record, so two concurrent `Add`s of one `ID` leave one version and a crash replays all of the replace or none of it. A WAL written by an earlier release, with separate add and remove records, still replays. Metadata may hold nested JSON objects and arrays, and `FlatStore` keeps a deep copy, so neither the caller nor a returned hit can change what it filters and snapshots; an `Add` whose metadata the WAL cannot encode (an unregistered Go type) fails before anything is removed. A batch that names one `ID` twice keeps the last `Document`, the way a second `Add` would. |
| `Snapshot()` | Write a unique owner-only temporary file beside `dir/store.snap`, then rename it atomically; truncate WAL. |
| `Add` / `Remove` every `SnapshotEvery` writes | Auto-triggers `Snapshot()`. |
| `Close()` | Final `Snapshot()`; close WAL; close store; release the lock. Afterwards `Add`, `Remove` and `Snapshot` return `ErrClosed`, and a second `Close` returns nil without touching the directory, which the next writer may already hold. |
| A write that fails after its WAL record (a keyword backend refusing a delete) | The write returns the error and the record is applied again before the next write and before any snapshot. While it still fails, writes, `Snapshot()` and `Close()` return the error and the WAL keeps the record, so the next `Open` replays it: a snapshot of the store without the document would have truncated the record that restores it. |

The snapshot's header records the embedder's `Name()` and `Dim()`, and every WAL add entry records the `Name()`, so writes that never reached a snapshot are checked on replay too. Reopening with a different embedder is refused with `*ModelMismatchError` because mixing vectors from different models is silently catastrophic for retrieval quality. To migrate models: drop the snapshot, re-index from source.

`SnapshotEvery` sets how many operations pass between automatic
snapshots; `0` means the default, 1000, and `-1` snapshots after every
operation. Explicit snapshots, recovery checkpoints and `Close()` also
persist state. Snapshot loading validates chunk counts and vector
dimensions before changing the store.

Replacing a document with empty text removes its previous chunks. Invalid
embedding dimensions, an empty vector, or non-finite coordinates are
rejected before removing the previous document, even from an embedder
whose `Dim()` is 0 until first use. Go declaration chunks include their documentation
comments; structural subchunks keep IDs derived from their global offsets.

Snapshots, the WAL, and the directory that holds them are owner-only
(0600 / 0700 plus `fileperm.Restrict`): they carry the full indexed
document corpus, which is app data, not a shared artifact.

Pluggable backends (chunker, embedder, store, keyword index) run under
recover guards on the indexing and watcher paths: a panicking
implementation surfaces as an attributed error from `Add` / `Remove` /
`scan` instead of killing the process.

## Hybrid retrieval

`Options.Keyword` injects a `KeywordBackend`. Two implementations ship:

- **`MemoryKeyword`**: in-process BM25-flavoured backend. Zero deps. Recommended default for `FlatStore`.
- **`PgVectorStore.Keyword()`**: Postgres full-text search over the pgvector table (below). The keyword leg for `PgVectorStore`.
- **`WrapSearchBackend(b search.Backend)`**: adapter over `battery/search` so an app that already runs Postgres FTS or a Bleve index reuses it.

When `Query.Hybrid=true` and `Options.Keyword != nil`, the index gathers the top-`4K` candidates from vector and keyword separately, fuses them with reciprocal-rank fusion (`k=60`), and feeds the union into MMR/rerank. With a `filter`, the keyword leg widens its window until enough candidates pass the filter, the backend runs out, or the window covers the whole store, so matches in scope are not cut by out-of-scope ones ranked above them. The keyword leg receives at most the first 64 terms of the query text, counted as runs of letters and Unicode numbers the way the keyword index tokenizes them (`H₂O` is one term), the same cap `battery/search` applies, so a token-stuffed query cannot buy an unbounded scoring sweep. When `Keyword == nil`, `Hybrid=true` silently degrades to vector-only.

## Diversity (MMR)

`Query.MMRLambda ∈ [0, 1]` runs Maximal Marginal Relevance over the candidate set:

- `λ = 0`: collapse to pure relevance order (no diversity).
- `λ = 0.3`: useful default; trims near-duplicates without sacrificing relevance.
- `λ = 1`: pick the most-diverse-from-already-picked items regardless of query.

`Query` returns an error for a non-finite lambda or one outside `[0, 1]`.

MMR needs the candidate vectors; the store keeps them on the chunks until the very end of the pipeline, then strips them before returning.

## Reranking

`Options.Reranker` is the second-stage scorer hook. The package ships **no** built-in reranker. Setting `Query.Rerank=true` without `Options.Reranker` is an error; silent quality loss is not allowed.

A reranker receives the original query text and the candidate hits; it returns a reordered slice with its own scores, and `Query` keeps its first `K`. Without MMR the candidates are the whole first-stage set, up to `max(4K, 50)` hits, so the reranker can promote a hit from below the top `K`; with `MMRLambda > 0` they are the `K` hits MMR picked. Typical wiring is a cross-encoder model behind an HTTP endpoint:

```go
type httpReranker struct{ url string }

func (r httpReranker) Rerank(ctx context.Context, q string, hits []semantic.Hit) ([]semantic.Hit, error) {
    // marshal {q, [hit.Text]} → POST → unmarshal scored slice → return
}
```

## Watcher

```go
w := semantic.NewWatcher(idx, semantic.WatchOptions{
    IncludeExts: []string{".go", ".md", ".markdown", ".txt"},
    ExcludeDirs: []string{".git", "node_modules", "dist", ".gofastr", "vendor"},
    PollInterval: 2 * time.Second,
    MaxFileSize:  1 << 20,
})
w.Run(ctx, "./src", "./docs")     // until ctx cancellation
// or
w.ScanOnce(ctx, "./src")          // one-shot
```

A file the chunker refuses (bytes that are not UTF-8, a fallback chunker that errors) is logged and left out of the index, along with any version of it indexed before; the scan goes on, and the watcher retries the file once it changes. An embedder or store error still stops `Run`.

Only regular files are indexed. Symlinks, devices, sockets and pipes under a root are skipped, so a link to `~/.ssh/id_ed25519` or `/dev/zero` is never read, and a link inside the root to another file there is not indexed twice. Each file is opened through an `os.Root` on its root, so a path swapped for a link out of the root between the walk and the read is refused, not followed. The open does not wait, so a file swapped for a FIFO is skipped instead of blocking the scan until a writer appears, and a root directory swapped for one fails the scan with an error. The read stops at `MaxFileSize`: a file that grew past it since the walk measured it, or was deleted, is skipped, and the document indexed for its path before is removed, whether this watcher or an earlier process indexed it. A root may name one file (`ScanOnce(ctx, "README.md")`, `gofastr semantic index README.md`); it is confined to the file's directory and indexes only that file.

A deleted file's document is removed on the next scan; if `Remove` fails, the scan returns the error and the scan after it tries again.

The first scan after a watcher starts also removes the documents it indexed in an earlier run for files under its roots that are gone, so a file deleted while the app was down leaves the index. Only documents whose ID is the watcher's own for that path are touched; documents the app added itself, and files under another watcher's roots, stay. An index that is not the one `Open` returns is not reconciled.

`Watcher` is polling-based on purpose: no third-party deps, deterministic behaviour, and the dev-time cost on a tree of a few thousand files is well under the embedding cost of any actual file edit. For very large trees, swap the implementation for fsnotify-backed without changing the public API.

`Index.Add` is replace-by-doc, so a re-emit of the same `Document.ID` cleanly replaces prior chunks. The watcher derives stable doc IDs from path hashes, so renames look like delete + add and bursty edits collapse into the next poll cycle.

## CLI

The `gofastr semantic` subcommand opens a per-cwd local snapshot at `~/.gofastr/semantic/<12-hex-chars-of-sha1(cwd)>/`. Different projects keep separate snapshots automatically.

```bash
gofastr semantic index .                                  # one-shot
gofastr semantic watch ./src ./docs                       # until SIGINT
gofastr semantic query "validate session token" -k 5 --hybrid
gofastr semantic query "router middleware" --mmr 0.4
gofastr semantic stats
gofastr semantic clear
```

`index` and `watch` hold the snapshot directory's writer lock, so a second
`index` or `watch` on the same directory fails with "another process has
this index open for writing" until the first exits. `query` and `stats`
open the directory read-only and run beside a `watch` in another terminal,
seeing every write it has logged. `clear` empties the snapshot directory
under the same writer lock and keeps the lock file, so it refuses while an
`index` or `watch` runs, and a writer that starts mid-clear waits for it.

The CLI indexes with the deterministic **stub** embedder unless you say
otherwise, which is fine for wiring a pipeline and useless for real retrieval,
and nothing on disk says which one produced a snapshot. Select the real one per
invocation:

| Variable | Effect |
|---|---|
| `SEMANTIC_BACKEND=ollama` | Use the Ollama HTTP embedder instead of the stub. `EMBED_BACKEND` is still read as a deprecated alias and warns. |
| `OLLAMA_URL` | Override `http://localhost:11434`. |
| `OLLAMA_MODEL` | Override `nomic-embed-text`. |

```sh
SEMANTIC_BACKEND=ollama gofastr semantic index .
```

Switching backends changes the vectors, so the next `query` against a snapshot
built by the other embedder returns a `ModelMismatchError` rather than quietly
poor results.

When `GOFASTR_URL` is set, `query` and `stats` are dispatched to that running server's `/semantic/*` routes. `index`, `watch`, and `clear` are always local: they mutate state and we don't want two writers fighting for the same file.

The routes require a bearer token (see below), so set
`GOFASTR_SEMANTIC_TOKEN` to the token the mount passes to `WithAuthToken`;
the CLI sends it as `Authorization: Bearer <token>`. It goes to the
`GOFASTR_URL` server only: the CLI refuses a redirect instead of following
it, so `GOFASTR_URL` must name the server itself, not a host that
redirects to it.

```sh
GOFASTR_URL=https://app.example.com GOFASTR_SEMANTIC_TOKEN=… gofastr semantic stats
```

## HTTP routes

**Every route requires a bearer token.** The handler fails CLOSED: with
no token configured it rejects every request with 401, so an
accidentally-unprotected mount cannot expose the index. Configure one
with `WithAuthToken` (or `Plugin.WithAuthToken`) and send
`Authorization: Bearer <token>`; the comparison is constant-time.
`WithInsecureDisabledAuth()` turns auth off for local development only.

The statuses below are what an *authenticated* caller sees; without a
valid token every one of them is 401.

| Method | Path | Body | Status |
| --- | --- | --- | --- |
| POST | `/semantic/index` | `{"documents":[{"id","source","text","metadata"}…]}` | 202 |
| POST | `/semantic/query` | `Query` (`text`, `k`, `filter`, `hybrid`, `mmr_lambda`, `rerank`) | 200 |
| GET  | `/semantic/stats` | — | 200 |
| DELETE | `/semantic/doc/{id}` | — | 204 |

Mount the plugin onto a `framework.App`:

```go
app.RegisterPlugin(semantic.NewPlugin(idx).
    WithPrefix("/semantic").
    WithAuthToken(os.Getenv("SEMANTIC_TOKEN")))
app.InitPlugins()
```

Or wire the bare `http.Handler` onto a `core/router.Router` or stdlib `http.ServeMux`:

```go
mux.Handle("/semantic/", http.StripPrefix("/semantic",
    semantic.Handler(idx, semantic.WithAuthToken(os.Getenv("SEMANTIC_TOKEN")))))
```

`filter.meta_match` (`Filter.MetaMatch`) is per-key equality on every store:
each key must be present in the chunk's metadata with a value equal to the
filter's, compared as JSON. `5` equals `5.0` (a Go `int` stored, a JSON number
queried), `[]` matches only an empty array (never "any array"), and `null`
matches a stored null, not a missing key. Numbers compare exactly, past
float64 precision too: tenant `2^53+1` never matches a filter on `2^53`. On
`PgVectorStore` the hybrid keyword leg checks the filter in SQL, as the vector
leg does, rather than on metadata decoded to float64. The HTTP routes keep
that precision: `/index` and `/query` decode JSON numbers exactly, so a
document indexed over HTTP with tenant `2^53+1` matches only that tenant. Hit
metadata `PgVectorStore` returns still decodes numbers as float64.

A document whose embedding is the zero vector (text with no tokens) scores 0
on every store, and both stores rank it at score 0: above a negative match,
level with an orthogonal one. A response the route cannot encode answers 500 with an error body,
never a 200 with an empty one. `POST /index` answers 400 for a document with
no id (`doc_id_is_required`) or one the chunker refuses
(`document_unchunkable`); the chunker's error text stays in the log.

`POST /index` and `POST /query` decode their bodies strictly: duplicate or
case-folded top-level JSON keys are refused with 400 rather than silently
resolved last-wins, so a body-mangling middlebox cannot index or query a
different value than an audit log recorded.

## Agent inventory

Importing `battery/semantic` registers a snippet in `agentsinv` (the process-wide agent-onboarding registry). Any binary that blank-imports the package, including the built `gofastr` CLI, will include the semantic entry in the generated `AGENTS.md` output from `gofastr agents sync`. The snippet lives in `battery/semantic/agents.md` and describes when to use the battery, the import path, and the key anti-patterns.

## Kiln integration

`kiln.Loop` gained a `ContextHook func(ctx, userText) string`. It is called once per turn with the most recent user message, and its return value is prepended to the provider's system prompt. The helper `agent.NewSemanticContextHook(idx, k)` wraps a `semantic.Index`:

```go
loop := &agent.Loop{
    Provider:    provider,
    Tools:       tools,
    ContextHook: agent.NewSemanticContextHook(idx, 6),
}
```

For every user turn, the hook runs a `Hybrid + MMR=0.3` query against the index and injects the top-6 chunks as a `# Project context` block. Retrieval errors degrade silently to an empty preamble so a misbehaving index never blocks the agent loop.

## Local dev: Docker + live tests

The package's default `go test ./...` covers the pipeline with the stub embedder + an `httptest` Ollama mock. To exercise the *real* semantic path (paraphrase clustering, real retrieval on a real corpus, MMR diversity that actually depends on real geometry), use the live-tagged suite:

```bash
make ollama-up        # docker compose up + pull nomic-embed-text on first run
make semantic-live     # go test -tags=live -v ./battery/semantic/...
make ollama-down      # tear down the container
make ollama-logs      # tail Ollama logs while debugging
```

`docker-compose.yml` ships at the repo root with a single `ollama` service exposing port `11434` and bind-mounting `./.ollama/` (gitignored) so the ~270 MB model file persists between container restarts and stays out of the repo.

The live suite (in `battery/semantic/live_test.go`, guarded by `//go:build live`) asserts:

| Test | What it verifies |
| --- | --- |
| `TestLive_OllamaProbe` | Server reachable, model returns embeddings, dim auto-detected. |
| `TestLive_SemanticSimilarity` | Paraphrase pairs cluster; cross-intent pairs don't; the property the stub embedder cannot satisfy. |
| `TestLive_IndexRetrievalParaphrase` | A 6-doc corpus + a paraphrase query surfaces the right doc at rank #1, both vec-only and hybrid. |
| `TestLive_MMRImprovesDiversityOnNearDuplicates` | MMR top-3 on a near-duplicate corpus surfaces ≥2 distinct topics. |
| `TestLive_PersistenceSurvivesProcessRestart` | Snapshot + reopen with a fresh embedder instance preserves retrieval. Model fingerprint must match. |
| `TestLive_WatcherFeedsRealEmbedder` | Polling watcher round-trips files through a real embedder. |

`make semantic-live` refuses to run if `http://localhost:11434/api/tags` doesn't respond; it tells you to `make ollama-up` first.

## Scale targets

| Corpus | Vector RAM (384-dim float32) | Brute-force query latency (Apple M-class) |
| --- | --- | --- |
| 10k chunks | ~15 MB | <1 ms |
| 100k chunks | ~150 MB | ~3 ms |
| 1M chunks | ~1.5 GB | ~30 ms (consider int8 quantization or ANN) |

Numbers above are from the `BenchmarkEmbed_*` family in `battery/semantic/bench_test.go` (corpus=10000 measured at ~3.3 ms per query on M4 Pro). Persistence costs scale linearly with chunk count; a 100k-chunk snapshot is ~200 MB on disk.

## Embedders

Two are wired:

- **`StubEmbedder`**: deterministic FNV bag-of-words, no deps, low retrieval quality. Fine for tests and offline development. Not a real model.
- **`OllamaEmbedder`**: HTTP client against a locally running Ollama-compatible server. Default `nomic-embed-text` (768-dim) gives real semantic retrieval. Recommended production default.

`OllamaEmbedder.Embed` rejects empty vectors, inconsistent dimensions,
changes from the configured or previously detected dimension, and responses
larger than 10 MiB. A rejected batch does not set the cached dimension.

For anything else (OpenAI Embeddings API, a private microservice, a CGO-bound model), implement the three-method `Embedder` interface and pass it via `Options.Embedder`. The rest of the package is embedder-agnostic.

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/battery/semantic"
import "context"
var ctx = context.Background()
import "log"
-->
```go
// Swap from stub to Ollama:
embedder := semantic.NewOllamaEmbedder(semantic.OllamaConfig{
    BaseURL: "http://localhost:11434",   // default
    Model:   "nomic-embed-text",         // ollama pull nomic-embed-text first
})

// Optional: confirm the server is reachable and warm the dim cache.
if err := embedder.Probe(ctx); err != nil {
    log.Fatalf("ollama unreachable: %v", err)
}
```

## Durable store (pgvector)

The default [FlatStore] is in-process: fast, zero-dep, and the right choice for a single-replica app. Its limitation (called out above under "Why this is in-process by default") is that the index lives in one process's RAM. A **shared, durable** index means multiple app replicas hitting the same vectors, or survival across restarts with no snapshot/WAL plumbing. When you need that, use `PgVectorStore`, a [Store] backed by a Postgres table with a [pgvector](https://github.com/pgvector/pgvector) embedding column.

```go
embedder := semantic.NewOllamaEmbedder(semantic.OllamaConfig{Model: "nomic-embed-text"})

store, err := semantic.NewPgVector(db, semantic.PgVectorConfig{
    Dim:   embedder.Dim(), // REQUIRED: pgvector columns are fixed-dim
    Table: "embed_chunks",  // optional, defaults to "embed_chunks"
})
if err != nil { return err }
if err := store.EnsureSchema(ctx); err != nil { return err } // creates extension + table

idx, err := semantic.Open(semantic.Options{
    Embedder: embedder,
    Store:    store,
    Keyword:  store.Keyword(), // full-text search over the same table
})
```

**The keyword leg reads the table.** `store.Keyword()` answers hybrid
queries with Postgres full-text search over the rows the vector leg ranks,
so a document one replica writes is in every replica's keyword leg at once.
It matches any query term against `to_tsvector('simple', text)` (no
stemming) and ranks by `ts_rank`; fusion uses rank order only. Terms are
split the way `MemoryKeyword` splits them, runs of letters and digits, so a
token Postgres keeps whole (an email address, a URL, `3.14`) is not matched
by its parts. The search scans the table; for a large one, add the index
the query can use:

```sql
CREATE INDEX ON embed_chunks USING gin (to_tsvector('simple', text));
```

`Open` refuses `MemoryKeyword` with a `PgVectorStore`: it would hold only
the writes of the process that made it, so replicas' hybrid results would
drift apart. It also refuses one store's `Keyword()` paired with another
store. A host's own in-process `KeywordBackend` is filled from the table's
rows at `Open`, with no deadline, and a read that fails fails `Open`.

**Replace is one transaction.** `Index.Add` and `Index.Remove` on a
`PgVectorStore` delete a document's old chunks and insert its new ones in a
single transaction. Metadata is marshalled before anything is deleted, and a
failed insert (metadata JSON cannot carry, such as `NaN`, or a NUL byte in the
text) rolls the delete back, so the old version stays. The transaction first
takes a transaction-scoped advisory lock (`pg_advisory_xact_lock`) per
document, keyed on the table's OID and the document ID, so two replicas
replacing the same document run one after the other and the table keeps one
whole version, even when one replica names the table `embed_chunks` and
another `public.embed_chunks`. The keyword backend is updated only after the transaction commits.

**Ranking parity.** `PgVectorStore` scores with pgvector's cosine-distance operator (`<=>`); because the embedder produces L2-normalized vectors, cosine equals the dot product `FlatStore` uses, so the two stores return the same top-K order given identical vectors (`Hit.Score = 1 − distance`). Default retrieval is brute-force cosine (correct, matches `FlatStore`); an operator can add an HNSW index for scale without code changes:

```sql
CREATE INDEX ON embed_chunks USING hnsw (embedding vector_cosine_ops);
```

**Extension note.** `EnsureSchema` runs `CREATE EXTENSION IF NOT EXISTS vector`. On managed Postgres (RDS, Cloud SQL) that is a one-time superuser action the app role often lacks: pre-install it once as admin. If the `vector` type is genuinely absent, `EnsureSchema` fails closed with an actionable error rather than proceeding.

**One model per table.** A pgvector column fixes the dimension and nothing
else, so `EnsureSchema` also creates `semantic_models`, one row per chunk
table naming the embedder (`Name()`) and dimension its vectors came from.
`Open` records the pair on a table's first open and returns
`*ModelMismatchError` when a later open names another while the table holds
rows; each write checks the row in its own transaction, so a replica still
running the old model cannot add to a table re-bound to a new one. To change
models, empty the table (`DELETE FROM embed_chunks`), open with the new
embedder, and re-add the documents; an empty table takes the new model. A
table filled before this check existed takes whatever model opens it first.
`Open` before `EnsureSchema` fails with an error naming `EnsureSchema`.

**No snapshot, no `Path`.** A Postgres table *is* the durable copy, so `PgVectorStore` deliberately does not implement the gob-snapshot capability. Pairing it with `Options.Path` fails closed at `Open` with an error; do not set `Path` with this store.

**Erasure.** `Document.Owner` names the user a document belongs to, and
`Index.Add` copies it onto every chunk (`Chunk.Owner`). `PgVectorStore`
registers a data eraser, so `App.EraseUserData` deletes the erased user's
chunks from the table. Any other index registers a store eraser (see
[Stores outside the database](data-export.md#stores-outside-the-database)):
after the SQL erasure commits it removes the user's documents, and with
`Options.Path` it rewrites the snapshot, which also empties the WAL, so no
copy of their text stays on disk. The registration stays until `Close`, so
close an index you are done with. An index opened `ReadOnly` registers
none, and neither does one over a custom `Store` that cannot list its
chunks. Hybrid search copies each chunk's text into the keyword
backend; a backend that implements `OwnedKeywordBackend`
(`IndexOwned(ctx, id, owner, text)`) receives the chunk's owner with it.
`WrapSearchBackend` does, so the copy lands as a `search.Document` with
`Owner` set and the search store's eraser removes it. `MemoryKeyword`
holds its copy in memory only. `PgVectorStore.Keyword()` keeps no copy:
it searches the chunk rows the eraser deletes.

**Honest trade-off.** `FlatStore` brute-force-scans a memory slice (~3 ms / 100k chunks on M-class, no network hop). `PgVectorStore` pays a Postgres round-trip per query but gains durability, multi-replica sharing, and a growth path to ANN indexing. Use `FlatStore` until the in-process limitation bites; switch when it does.

## Limitations and follow-ups

- The watcher does not honour `.gitignore`; only an explicit `ExcludeDirs` list. Glob-level ignore parsing is deferred.
- Two store backends ship: `FlatStore` (in-process, brute-force, default) and `PgVectorStore` (durable Postgres+pgvector, multi-replica). Out-of-process ANN services (dedicated HNSW/IVF servers) remain out of scope until benchmarks show brute-force losing; an HNSW index on the pgvector table covers the scale path today.
- Multiple named indexes per process are not supported; the design is one index per app, mirroring the `Options.Keyword` and `Options.Path` shape.

## Common mistakes

- **Switching embedders over an existing snapshot.** `Open` refuses
  with a `*ModelMismatchError`: vectors from different models are
  silently catastrophic for retrieval, so mixing is not allowed. To
  change models: drop the snapshot directory and re-index from source.
- **Setting `Hybrid: true` without `Options.Keyword`.** It silently
  degrades to vector-only: no error, just no keyword leg in the
  fusion. Wire `MemoryKeyword` (or `WrapSearchBackend`) if you want
  hybrid to actually be hybrid.
- **Setting `Rerank: true` without `Options.Reranker`.** Hard error:
  the package ships no built-in reranker and refuses silent quality
  loss. Implement the `Reranker` interface (typically a cross-encoder
  behind HTTP) before opting in.
- **Expecting real retrieval quality from `StubEmbedder`.** It's a
  deterministic FNV bag-of-words for tests and offline development;
  paraphrases will not cluster. Use `OllamaEmbedder` (or your own
  `Embedder`) for anything a user sees.
- **Pointing `index`/`watch`/`clear` at a running server.** Those
  subcommands are always local, even with `GOFASTR_URL` set: only
  `query` and `stats` dispatch to the server. Two writers on one
  snapshot directory is exactly what the split prevents.
