# Backend adoption benchmark

This benchmark measures how reliably a fresh Codex process can build and then
change the same small Go product with:

- GoFastr;
- Gin; and
- the Go standard library (`net/http`) as a control.

It is a backend-heavy vertical-slice benchmark, not a microbenchmark or a
complete full-stack adoption study. The measured unit is a working application
produced by an agent from a cold workspace. Every candidate receives the same
product contract in `TASK.md`; successful initial builds then receive
`MAINTENANCE_TASK.md`.

The server-rendered dashboard is checked for required content only. This
benchmark does not score visual quality, accessibility, responsive behavior,
multi-page product work, frontend interaction quality, or deployment. Do not
use it alone to rank full-stack frameworks or predict broad adoption.

## Why these three

Gin represents a widely recognized router-first Go framework. `net/http`
provides a control for how much an agent can accomplish without framework
abstractions. GoFastr represents the full-stack, AI-first approach under test.
This first phase intentionally avoids hosted platforms whose required runtime
or control plane would make local execution incomparable.

## What is measured

The harness records, for every independent trial:

- Codex CLI version and requested model;
- wall-clock duration and reported token usage;
- build and `go test ./...` status;
- black-box HTTP behavior through a real server process;
- session-cookie flags and cross-site request rejection;
- per-user data isolation over REST and MCP;
- OpenAPI discovery, server-rendered UI, SQLite persistence, and password
  storage;
- source lines and direct dependency count; and
- the same measurements after the maintenance task.

Correctness is scored by deterministic probes. Token use and elapsed time are
reported, but never compensate for missing or insecure behavior.

## Run

From the repository root:

```sh
go run ./evals/backend-adoption/cmd/backend-eval \
  -runs 2 \
  -concurrency 2 \
  -timeout 25m
```

Artifacts are written beneath `dist/backend-adoption/<timestamp>/`. Each cell
contains its isolated workspace, Codex transcript, final response, grade
reports, and server logs. The aggregate `results.json` and `RESULTS.md` are the
canonical result files.

Use `-model` to pin a model instead of using the Codex CLI default. Use
`-framework gofastr` (repeatable) or `-runs 1` for a smoke run.

To rerun deterministic maintenance probes without spending more agent tokens:

```sh
go run ./evals/backend-adoption/cmd/backend-eval \
  -regrade-maintenance dist/backend-adoption/<timestamp>
```

The first completed study is summarized in
[`results/2026-07-26-codex.md`](results/2026-07-26-codex.md).

## Integrity rules

- Candidate agents may inspect only their own workspace.
- Candidate workspaces never contain the grader or another candidate.
- All variants use the same Codex executable, model setting, timeout, prompt,
  and task contract.
- The GoFastr variant receives the normal files emitted by `gofastr init`.
  This is intentional: discoverability and agent guidance are part of the
  framework experience being evaluated.
- Runtime dependencies must be local. No external service or API key is
  allowed.
- A trial is not considered successful merely because it compiles.
