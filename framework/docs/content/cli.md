# The gofastr CLI

```bash
go install github.com/DonaldMurillo/gofastr/cmd/gofastr@vX.Y.Z
```

One binary. `gofastr <command> --help` prints every flag; this page maps
each command to the doc that covers it.

## Scaffold

- `gofastr init <name>` — a new project: framework UI, `DESIGN.md`, a
  sample entity (`--no-entity` to skip), git, and the agent onboarding
  files. `--module=<path>` sets the Go module; `--db=sqlite|postgres`
  picks the driver (default `sqlite`; `sqlite3` and `postgresql` are
  accepted aliases, anything else is an error). `init . --reinit` refreshes the
  AI-agent onboarding files in place (`--force` overwrites your edits);
  no Go code or git changes. A released CLI pins the generated `go.mod` to its
  matching GoFastr version. A local development build prints the exact
  `go get …@vX.Y.Z` step because it cannot infer a release safely.
- `gofastr new handler <name>` / `gofastr new route <path>` — scaffold
  one handler or route registration into an existing app.
- `gofastr agents init|sync|skill` — generate or refresh `AGENTS.md`
  and the per-battery detail files under `agents/`.
- `gofastr theme edit` — a local theme configurator: every token as a control,
  the whole component gallery as a live preview, write-back to `theme/theme.go`.
- `gofastr theme init` — scaffold a typed `theme/theme.go` you own
  (`--out=<path>` writes elsewhere; `--force` overwrites).

## Blueprints

- `gofastr validate <yml>` — validate a blueprint without generating
  (exit 0 = valid; includes the unscoped-PII lint).
- `gofastr generate --from=<yml>` — one-shot scaffold of the whole app
  as owned Go ([tutorial](tutorial-blueprint-app.md)). `generate --add`
  / `generate entity <name>` / `generate screen <name>` scaffold *new*
  files into an existing app; owned files are never touched.
  `generate --config=<codegen.yml>` runs the configurable codegen engine
  ([codegen](codegen.md)); `generate --watch` re-runs on every change.
  `generate all` is the full-project path (same engine as `--from`).
- `gofastr pack [app-dir]` — snapshot a generated app into a
  best-effort `gofastr.yml`. Lossy; not an inverse of `generate`.

## The daily loop

- `gofastr dev` — rebuild on save, browser livereload, contract findings
  for what you changed (after the reload, never blocking it), and the dev
  MCP tools for your coding agent ([dev-livereload](dev-livereload.md)).
  `--dir` sets the watch root, `--pkg` the main package under it,
  `--addr`/`-p` the port; `--no-a11y` skips the accessibility lint on
  each rebuild.
- `gofastr build` — codegen + `go vet` + accessibility lint + the embed
  server-action gate + contract verification + `go build`. Only
  error-severity contract findings stop the build (`gofastr verify` is
  the full report; an existing app adopts the gate with a baseline — see
  [contracts](contracts.md)). Flags: `--no-a11y` skips the a11y lint,
  `--no-embed-check` skips the embed gate, `--no-contracts` skips the
  contract gate, `--no-generate` skips codegen, `--pkg` selects the main
  package, and `-o`/`--output` names the binary (default `bin/server`).
  `--allow-unverified-embeds` keeps proven embed violations fatal while
  downgrading a surface the analyzer cannot follow ([embed](embed.md)).
- `gofastr test` — run the project's tests.
- `gofastr docs [topic]` — these docs, offline, versioned with the
  binary (`--list` every topic, `--grep <term>` to search).

## Ship

- `gofastr migrate up|down|status|generate|force` — versioned
  migrations: advisory-locked, checksum- and dirty-state-guarded
  ([migrations](migrations.md)).
- `gofastr generate cli` — a customer-facing terminal client for your
  API, with scoped API-token auth ([app-cli](app-cli.md)).
- `gofastr generate sdk` — Go + JS/TS clients your app can host behind
  a live docs page ([sdk](sdk.md)).
- `gofastr upgrade` — move to a newer release: lists every migration
  note between your `go.mod` version and the target (`--to vX.Y.Z`;
  without it the newest tagged release is resolved via the proxy) and
  points at affected lines; `--apply` runs the steps ([upgrading](upgrading.md)).

## Verify

- `gofastr verify [capability...]` — the contract analyzers: routing,
  permissions, security, data, entities, architecture, rendering,
  accessibility, performance, testing, ai. Strict by default; relax in
  `gofastr.contracts.yml` or waive one instance with
  `//gofastr:allow(RULE) reason` ([contracts](contracts.md)).
- `gofastr verify --list` / `--explain <rule>` — the rule catalog, and
  any one rule in full: why it matters, how to fix it, a worked example.
- `gofastr verify --json` / `--sarif <file>` — machine-readable output.
  Each JSON diagnostic carries its whole rule, so an agent acting on one
  finding needs no second call.
- `gofastr verify --fix` — apply the mechanical fixes, then re-verify.
- `gofastr verify --changed[=<ref>]` — report only findings in files this
  change touched; the analysis still runs whole-tree, so cross-file
  findings are still caught. For pre-commit hooks and PR review.
- `gofastr verify --strict --baseline-write` — record today's findings as
  accepted debt so only NEW ones fail. How an existing codebase adopts the
  gate.
- `gofastr verify --rule <id> --fix` — apply one rule's fixes at a time
  so edits stay reviewable; `--analyzer <name>` scopes to one analyzer,
  `--config <file>` picks a non-default config, `--no-vet` skips `go vet`.

## Audit

The `audit` subcommands predate `verify` and remain for the two things it
does not cover: a runtime browser scan, and the dependency report.

- `gofastr audit a11y --url <base>` — axe-core scan of a running app in
  both color schemes (`--email`/`--password` log in first). The *static*
  accessibility rules are part of `gofastr verify accessibility`.
- `gofastr audit lint` — the original AI-mistake scanner. Superseded by
  `gofastr verify security data`, which covers the same rules with a
  reason and a fix attached to each.
- `gofastr audit deps` — list dependencies that perform init-time
  global registrations.

## Extras

- `gofastr semantic index|watch|query|stats|clear` — the local semantic
  index ([semantic search](semantic-search.md)).
- `gofastr harness` — the experimental agent harness (`harness mcp`
  runs it as a stdio MCP server; `harness creds` manages encrypted API
  keys).
- `gofastr version` — print version info.

## Common mistakes

- **Updating `go.mod` but not the CLI** (or the other way around). They
  version independently — after `go get …@vX.Y.Z`, also
  `go install …/cmd/gofastr@vX.Y.Z`. `gofastr upgrade --apply` keeps
  them in step ([upgrading](upgrading.md)).
- **`generate --force` on an app you've edited.** It regenerates the
  *entire* set and discards your changes. To add to an existing app use
  `generate --add` / `generate entity <name>` — owned files are never
  touched.
- **`dev --pkg ./cmd/server` from the wrong directory.** Keep `--dir`
  at the project root and point `--pkg` below it; otherwise the watcher
  misses `internal/` and relative paths (a sqlite `db_url`, static
  dirs) resolve against the command directory.
- **`migrate force` as a routine fix.** It only rewrites the tracking
  table. It's for dirty-state recovery or adopting a baseline — read
  [migrations](migrations.md) first.
