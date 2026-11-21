# The gofastr CLI

One binary covers scaffolding, hot reload, migrations, contract verification, SDK/CLI generation, static exports, deployment, and offline docs.

## Install

With Go 1.27.4+:

```bash
go install github.com/DonaldMurillo/gofastr/cmd/gofastr@latest
```

Without a Go toolchain, use the prebuilt binary installer from [GitHub Releases](https://github.com/DonaldMurillo/gofastr/releases):

```bash
# macOS / Linux
curl -fsSL https://github.com/DonaldMurillo/gofastr/releases/latest/download/install.sh | sh
```

```powershell
# Windows
irm https://github.com/DonaldMurillo/gofastr/releases/latest/download/install.ps1 | iex
```

Run `gofastr help` for an overview or `gofastr <command> --help` for every flag.

## Scaffold

- **`gofastr init <name>`** (or `gofastr init .`): scaffold a flat GoFastr app—`main.go`, `screens.go`, `DESIGN.md`, `gofastr.settings.yml`, `.env`, git, and agent onboarding files (`AGENTS.md`, `CLAUDE.md`, `.agents/`).
  - `--no-entity`: skip the sample `posts` entity, auth screens, and database wiring for a barebones start.
  - `--db=sqlite|postgres`: pick the database driver (default `sqlite`).
  - `--module=<path>`: set the Go module path.
  - `--reinit`: refresh agent onboarding files in an existing project without touching Go code (`--force` overwrites edits).
- **`gofastr new handler <name>`** / **`gofastr new route <path>`**: scaffold a single handler file or print a validated route registration (`--method`, `--path`, `--handler`).
- **`gofastr agents init|sync|skill [dir]`**: generate or refresh `AGENTS.md`, `.agents/`, and the coding-agent skill (`skill --force` overwrites a hand-edited skill).
- **`gofastr theme init`** / **`gofastr theme edit`**: write an owned `theme/theme.go` (`--out`, `--force`) or launch the live browser theme editor (`--addr`).

## Daily development

- **`gofastr dev`**: run the dev loop—rebuilds on save, live-reloads the browser, runs contract checks on changed files, and mounts dev MCP tools for Claude Code or Codex ([dev-livereload](dev-livereload.md)).
  - Flags: `--dir <path>`, `--pkg <pkg>`, `--addr <host:port>` / `-p <port>`, `--no-a11y`.
  - While building or after a compile error, the server responds `503` with the build error rather than dropping connections.
- **`gofastr build`**: run codegen, `go vet`, accessibility checks, embed checks, contract verification, and `go build` (default output `bin/server`).
  - Flags: `-o`/`--output`, `--pkg`, `--no-generate`, `--no-a11y`, `--no-embed-check`, `--no-contracts`, `--allow-unverified-embeds`.
- **`gofastr test [flags] [packages]`**: run Go tests (`./...` by default). Supports `--run`, `--bench`, `--timeout`, `--race`, `--cover`, `--short`.
- **`gofastr docs [topic]`**: browse embedded docs offline (`--list`, `--grep <term>`).
- **`gofastr docs serve`**: serve the docs site locally on `127.0.0.1:8083` (`--open`, `--port`, `--release vX.Y.Z`, `--refresh`, `--dir <path>`, or `--full` for live interactive demos).

## Verification & audits

- **`gofastr verify [capability...]`**: check your app against the contract rules covering routing, permissions, security, data, entities, architecture, rendering, accessibility, performance, testing, and AI ([contracts](contracts.md)).
  - `--list` / `--explain <rule>`: inspect rules, rationale, and worked fixes.
  - `--fix` / `--rule <id> --fix`: apply mechanical fixes and re-verify.
  - `--changed[=<ref>]`: report only findings in touched files (for pre-commit hooks and PR checks).
  - `--strict --baseline-write`: baseline existing debt so only new findings fail.
  - `--json` / `--sarif <file>`: machine-readable output (each JSON finding includes its full rule text).
  - `--analyzer <name>`, `--config <file>`, `--no-vet`.
- **`gofastr audit a11y --url <base>`**: run a live axe-core accessibility scan in light and dark mode (`--email`, `--password -` reads from `GOFASTR_AUDIT_PASSWORD` or stdin).
- **`gofastr audit deps`**: list dependencies with `init()` global registrations and any [unmounted](unmount.md) packages.
- **`gofastr audit lint`**: legacy security/SQL scanner (superseded by `gofastr verify security data`).

## Generators & packages

- **`gofastr validate <gofastr.yml>`**: validate an app spec (including unscoped-PII checks) without writing files.
- **`gofastr generate app <gofastr.yml>`**: one-shot scaffold of a full app as plain Go you own ([app spec tutorial](tutorial-app-spec.md)). Use `--add`, `generate entity <name>`, or `generate screen <name>` (`--from-a11y=<file>`) to add new files without touching existing ones.
- **`gofastr generate [--config <file>] [--watch]`**: run configured generators from `gofastr.settings.yml` ([codegen](codegen.md)).
- **`gofastr generate styles [patterns]`**: check `*.tokens.css` and `*.style.css` files against theme tokens, emit typed `*_tokens.gen.go` / `*_style.gen.go` bindings, and write `.gofastr/tokens.css` for editor completion ([theming](theming.md#app-tokens-in-css-nametokenscss)).
- **`gofastr generate package [<name>]`**: copy a canonical chrome package (`siteheader`, `sitefooter`, `docpage`) into your module as owned code (`--out`, `--dry-run`).
- **`gofastr generate client`**: rebuild `entities/client/client.go` from `entities/*.go` (`--force`, `--dry-run`, `--json`).
- **`gofastr generate sdk`**: generate a Go module and JS/TS client from your entities or `--from-openapi <path|url>` ([sdk](sdk.md)).
- **`gofastr generate cli`**: generate a branded customer terminal client with scoped API-token auth ([app-cli](app-cli.md)).
- **`gofastr pack [app-dir]`**: export a best-effort, lossy `gofastr.yml` snapshot of an app's declarable shape.
- **`gofastr unmount <package>`** / **`gofastr remount <package>`**: copy a GoFastr package into your repo to customize it, or switch back ([unmount](unmount.md)).

## Database & shipping

- **`gofastr migrate up|down|status|generate|force|repair`**: manage versioned SQL migrations with checksum and dirty-state guards ([migrations](migrations.md)); `repair` fixes legacy SQLite owner keys ([SQLite driver](sqlite-driver.md)).
- **`gofastr export [--base /sub] [--pkg ./cmd/web] <dir>`**: render every route to static HTML + assets (and site search index if mounted) ([static export](static-export.md)).
- **`gofastr deploy <user@host>`**: cross-build for Linux, copy over SSH, install/restart a systemd unit, and verify `/healthz` with automatic rollback on failure ([deploy](deploy.md)).
  - Subcommands: `deploy rollback <user@host>`, `deploy status <user@host>`.
  - Flags: `--name`, `--pkg`, `--bin`, `--env-file`, `--port`, `--health`, `--wait`, `--keep`, `--ssh-port`, `-i`.
- **`gofastr release --version=<semver> [--pkg <pkg>] [--url <https-url>]`**: cross-build macOS/Linux/Windows archives, `checksums.txt`, and `install.sh` / `install.ps1` installers (`--service` wires OS service install) ([release](release.md)).
- **`gofastr upgrade [--to vX.Y.Z] [--from vX.Y.Z] [--apply]`**: list breaking changes and exact affected source lines between your `go.mod` version and the target release; `--apply` updates `go.mod` ([upgrading](upgrading.md)).

## Telemetry, desktop & extras

- **`gofastr telemetry stats|logs|requests|issues|request`**: inspect on-disk logs and traces written by `battery/telemetry`, even when the app is stopped ([telemetry](telemetry.md)). Supports `--dir`, `--app`, `--since`, `--limit`, `--route`, `--level`, `--grep`, `--failed`, `--slow`, `--json`.
- **`gofastr desktop build|install|uninstall`**: build pure-Go OS WebView binaries/bundles (`--format=tar.gz` on Linux) and manage per-user Linux installs ([desktop](desktop.md#the-gofastr-desktop-verbs)).
- **`gofastr semantic index|watch|query|stats|clear`**: manage the local semantic search index ([semantic search](semantic-search.md)).
- **`gofastr harness`**: experimental agent harness (`harness mcp`, `harness creds`).
- **`gofastr version`** (`--version`, `-v`): print the installed CLI version.

## CLI behavior & safety

- **Version mismatch check:** Project commands compare your `go.mod` GoFastr version against the installed CLI and warn on stderr if they differ (silence with `GOFASTR_NO_VERSION_CHECK=1`).
- **Machine-readable `--json`:** Commands supporting `--json` write one JSON document to stdout and all prose/warnings to stderr (`generate --watch --json` streams newline-delimited JSON).
- **Project confinement & safe output:** Scaffold commands (`init`, `generate app`, `generate package`) refuse to follow symlinks outside the project root and never overwrite existing files unless `--force` is passed (derived generators like `generate styles`, `generate client`, `generate cli`, and `generate sdk` rebuild their derived outputs on each run). External text is scrubbed of terminal control and bidi sequences before printing.
- **Windows `init` double-invocation:** `gofastr init <name>` exits 1 if the destination already exists and leaves it untouched. If a wrapper or agent runner reports "already exists" alongside a complete scaffold, check whether the runner retried the command after it already succeeded rather than passing `--force`.

## Common mistakes

- **Updating `go.mod` without updating the CLI.** They version independently. After `gofastr upgrade --apply`, run `go install github.com/DonaldMurillo/gofastr/cmd/gofastr@vX.Y.Z` so codegen and checks match.
- **Running `generate app --force` on hand-edited code.** `--force` overwrites the entire generated set. To add new files safely, use `generate app --add`, `generate entity <name>`, or `generate screen <name>`.
- **Running `gofastr dev` from a subdirectory instead of the repo root.** Run from the project root and pass `--pkg ./cmd/server` so file watching covers `internal/` and relative paths (SQLite DBs, `static/`) resolve cleanly.
- **Using `migrate force` for routine schema fixes.** It only updates the migration tracking table; use it only for dirty-state recovery or baselining ([migrations](migrations.md)).
