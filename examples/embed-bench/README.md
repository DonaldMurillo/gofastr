# embed-bench

A benchmark comparing two ways an agent (Claude Code-style) might "find things in this repo":

- **ripgrep** with a tight, human-chosen keyword set (what Claude does at runtime — pick tokens before grepping).
- **`gofastr embed query`** with the natural-language question, no preprocessing.

## Run it

```bash
ollama serve &                 # or: brew services start ollama
ollama pull nomic-embed-text   # one-time
make build                     # build dist/gofastr
EMBED_BACKEND=ollama dist/gofastr embed index ./battery ./docs ./core ./framework ./kiln
go run ./examples/embed-bench
```

## How it scores

| Method | Time | "Found" means |
| --- | --- | --- |
| ripgrep | wall clock for the `rg -l ...` call | the target file's path appears anywhere in the unranked match list |
| embed | wall clock for `gofastr embed query ... --hybrid` | the target file appears in the top-5 ranked chunks |

The keyword set passed to `rg` per query is the *best* set of literal terms a human/agent would pick — this is the favourable case for ripgrep. A naive whole-question grep performs much worse.

## What the run on this repo says

Sample output (`nomic-embed-text` on Apple M-class CPU, 381 docs / 5270 chunks indexed):

```
query                             rg time   rg hits  rg ✓?   embed t   embed rank
auth: middleware                  26ms      121      ✓       293ms     miss
cache: TTL eviction               10ms      31       ✓       296ms     1 ✓
SSE: server-pushed updates        10ms      47       ✓       391ms     miss
embed: how does RRF fusion work   10ms       4       ✓       208ms     miss
kiln: tool dispatch               10ms      33       ✓       260ms     1 ✓
migrate: schema diff              10ms      44       ✓       360ms     3 ✓
openapi: route generation         10ms      45       ✓       235ms     1 ✓

avg ripgrep:  12 ms
avg embed:    285 ms
both found target:   4/7
only ripgrep found:  3/7
only embed found:    0/7
```

## Interpreting the misses

When embed "misses", the printed top-3 is usually *more* relevant than my hardcoded `targetGlob` — the ground truth, not the embedder, is what's wrong. The three misses on this run:

- **auth: middleware** — target was `battery/auth/session_middleware.go`. Embed returned `core/router/router.go`, `framework/integration_test.go`, `framework/app.go`. Adding auth middleware to routes is about the router/middleware machinery, not the auth-specific file. Defensible.
- **SSE: server-pushed updates** — target was `events.md`. Embed returned `docs/widgets.md` (which does discuss server-pushed updates for widgets) and `core/stream/sse.go` (the actual SSE implementation). Both are arguably more answer-shaped than `events.md`.
- **RRF fusion** — target was `battery/embed/hybrid.go`. Embed returned `battery/embed/README.md` and `docs/embed.md` which both *explain* RRF fusion. For a conceptual question, the docs that explain it beat the code that implements it.

The bench is honest about this: every miss is followed by the actual top-3, so you can decide whether to trust your `targetGlob` or the embedder.

## What this argues

- **ripgrep wins on time** by ~24× — millisecond-class versus a few hundred ms. For "I know the exact word", grep is unbeatable.
- **embed is competitive when the question is conceptual** — `nomic-embed-text` returns ranked, chunk-level answers with their text inline; the agent doesn't have to open N files to skim.
- **The two are complements**, not substitutes. The framework's pitch: pair the existing `bash → rg` reflex with one `embed query` call when the question is "how does X work" rather than "where is X defined". Time-to-answer beats raw search latency when the agent doesn't have to skim 40+ files.

## Limitations

- Single-machine timing; no warm/cold cache control.
- The `targetGlob` ground truth is hand-picked and can disagree with reasonable retrieval.
- ripgrep's keyword set is human-chosen; this favours `rg`. An honest agent run would have to spend tokens picking those keywords, which the bench doesn't charge against `rg`.
- Embed timing includes the CLI's per-invocation snapshot load (~150 ms on this corpus). A long-lived process (`framework.App` with the plugin mounted) skips that overhead.
