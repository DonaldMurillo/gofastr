# Contributing to GoFastr

Issues and PRs are welcome, but expect strong opinions about scope: the goal
is a framework an AI agent can drive end-to-end, not a kitchen-sink CMS. If
you're unsure whether a change fits, open an issue first.

Want to validate GoFastr in a real non-author application? Use the
[external pilot checklist](docs/pilot-program.md) so the result produces
comparable evidence for the v1 adoption gate.

Security bugs: see [SECURITY.md](SECURITY.md). Do not open a public issue.

## Prerequisites (the honest list)

- **Go 1.27.0+** (`go.mod` selects the preferred `go1.27.2` toolchain).
- **Docker**: the Postgres suites need a server. `make postgres-up` starts
  the `postgres` service from `docker-compose.yml` (pgvector/pgvector:pg16,
  which also covers `battery/semantic`), and `make test-pg` starts it and runs
  against it. Without one, the Postgres halves of the dual-dialect subtests
  skip; SQLite still runs. (Alternative: point `TEST_POSTGRES_DSN` at any
  Postgres and use `make test-pg-env`, no Docker needed.)
- **Chrome/Chromium**: the chromedp end-to-end suites (`examples/site`,
  `core-ui/runtime`, `kiln/integration`) drive a real browser.

## Building

```sh
make build       # → dist/gofastr, dist/kiln
make build-all   # also builds every example into dist/examples/
```

`dist/` is the **only** sanctioned build output location and is gitignored.
The dev loop (`./scripts/dev-watch.sh`, port `:8082`) writes binaries to
`/tmp` because the watched tree must stay clean.

**Never commit binaries.** Audit before every commit:

```sh
find . -maxdepth 3 -type f -size +500k ! -path "./.git/*" ! -path "./dist/*" ! -name "*.go" ! -name "*.md"
```

Anything that prints is a stray binary: move it to `dist/` or delete it.

## Testing

The full gate (build + vet + test, cache bypassed, generous timeout):

```sh
./scripts/test-all.sh                  # full run
RACE=1 ./scripts/test-all.sh           # with race detector (~2x slower)
SHORT=1 ./scripts/test-all.sh          # -short, skips the slow suites
./scripts/test-all.sh ./core-ui/...    # scope to a subtree
```

Quick iteration: `go test ./path/to/pkg/` on the packages you touched, plus
`make test` (`-short ./...`) for a fast sweep.

On Windows, run the same package groups through PowerShell:

```powershell
./scripts/test-windows.ps1                 # all packages; browser suites run alone
./scripts/test-windows.ps1 -Suite unit     # packages outside the browser group
./scripts/test-windows.ps1 -Suite browser  # isolated browser group and CLI e2e
```

The Windows script writes package lists, the commit, and test output under
`dist/windows-tests/`. It continues after failures and exits with an error
if any package fails. The Windows CI job also builds every package and runs
vet with `unsafeptr` disabled for the native COM pointer conversions.
CI provisions PostgreSQL 17 and builds the pinned pgvector extension with
Visual C++ through `scripts/ci-windows-postgres.ps1`. Database tests are required
in CI. For local database tests, set `TEST_POSTGRES_DSN` to your test database.
Text files use LF on every platform so generated asset hashes stay consistent.
CI partitions unit packages across four jobs and gives each browser package its
own job. The site accessibility checks run separately from the other site tests.
The native Windows browser group also isolates `framework/ui`.
Site accessibility scans use two shards, matching the Linux gate.
The nested `battery/print/chromepdf` module has its own build, vet, and test
step because the root module's `./...` pattern does not include it.

### Known isolation rules

Some suites are reliable alone but flake when sharing a `go test` run:

- `examples/site` chromedp e2e and the axe accessibility gate
  (`TestAxe_AllPagesAreClean`) must run **isolated**, e.g.
  `go test ./examples/site/ -run TestAxe_AllPagesAreClean`.
- `core-ui/runtime` and `core-ui/widget` flake under parallel load. Rerun
  them isolated before concluding you broke something.

`test-all.sh` caps package parallelism at 2 for the same reason (ephemeral
port exhaustion); override with `TEST_PARALLELISM=N` at your own risk.

For a fix: write the failing test first, confirm it fails for the right
reason, then fix. Keep test names short and concrete.

## Before sending a PR

```sh
make hooks            # activates .githooks (commit-msg, pre-commit, pre-push)
gofmt -l .            # must print nothing
go vet ./...
```

Run the binary audit above, and `./scripts/test-all.sh` if your change is
more than a local tweak.

## Commit style

Conventional-ish, matching the existing log and `CHANGELOG.md`
(Keep a Changelog): `feat(scope): …`, `fix: …`, `docs: …`, `chore: …`, with
`!` for breaking changes (e.g. `feat(cli)!: …`). One theme per commit, no
mega-commits.

## Where to start reading

- `CLAUDE.md`: repo working agreements (humans benefit too).
- `core-ui/ARCHITECTURE.md`: mandatory before any UI/runtime work.
- `framework/ARCHITECTURE.md`: mandatory before touching `framework/` layout.
- `framework/docs/content/*.md`: the user-facing docs, embedded into the
  `gofastr` binary; docs ship in the same commit as the code they describe.
