# Claude / agent instructions for the GoFastr repo

**Before changing a file, run `go run ./cmd/primitives for <path>`.** It
prints the primitives that own the file with their invariants (each
naming the test or analyzer that enforces it) and decisions. The registry
lives in `primitives/`, one `primitive.yml` per primitive, format in
`primitives/README.md`; `go run ./cmd/primitives lint` checks it and `gen` writes
the import tests it declares. A rule or a "why" that would have gone
into a comment or into this file goes there instead (the `primitives-sweep`
skill).

Read before you start:
- UI, the runtime, `framework/uihost`, or anything that emits markup or
  CSS (the app generator, batteries): `core-ui/ARCHITECTURE.md`.
- Adding, moving or extracting anything under `framework/`:
  `framework/ARCHITECTURE.md`.
- An exported API, route, CLI subcommand, JSON field or generated
  artifact changes: the `gofastr-docs` skill. Docs ship in the same
  commit, in `framework/docs/content/*.md`.
- Commands (build, test, analyzers, red probes, dupl), the full text of
  the process rules, and where each kind of code lives:
  [`docs/contributing.md`](docs/contributing.md).

## TL;DR of the architecture

SSR first, then hydration, never a re-render. Cross-page navigation is
client-side; in-page state is an island or the query string. Passive
freshness polls (`data-cui-poll`); SSE on the one `/__gofastr/sse` bus is
the last resort. Server state lives in the DB or the client signal store,
never in server RAM; sessions are signed tokens. The full ladder is
`framework/docs/content/reactivity.md`.

## Hard rules

Code cites these by number. Each code rule's full text, and what enforces
it, is its registry entry.

1. In-page state stays on its route: `uihost/in-page-state-stays-on-its-route`.
2. No pagination, sort or filter math in JS: `runtime/no-list-math-in-js`.
3. SSE is push-only, on the one bus: `runtime/sse-is-push-only`.
4. No `location.href` or full reloads as a fix: `runtime/no-hard-navigation`.
5. A new `data-cui-*` attribute is documented and tested: `runtime/attributes-are-documented`.
6. Per-user data under auto-CRUD sets `Scope.OwnerField`: `crud.scope/per-user-entity-is-scoped`.
7. One styling surface, zero bespoke CSS: `ui.styling/one-styling-surface`.
8. Survey `framework/ui` and `core-ui` before building: `ui/survey-before-building`.
9. Judge UI by screenshots of `examples/meridian`, never by DOM probes: the `on_change` line of `ui`.
10. Triage every review thread before merging: `./scripts/pr-review-findings.sh <N> --gate`.
11. Prove a guard: break it, watch its test fail, put it back.
12. Watch every PR you open (`subscribe_pr_activity`); merge main in on a conflict, never force-push.
13. Merge only after a reviewer has read the PR: a person, a review bot, or an adversarial review by agents that did not write it.

Rules 10 to 13, and 7 to 9 at length, are in `docs/contributing.md`.
