# Contributing to GoFastr

Issues and PRs are welcome, but expect strong opinions about scope: the goal
is a framework an AI agent can drive end-to-end, not a kitchen-sink CMS. If
you're unsure whether a change fits, open an issue first.

Security bugs: see [SECURITY.md](SECURITY.md) — do not open a public issue.

## Prerequisites (the honest list)

- **Go 1.26+** (`go.mod` says `go 1.26.4`).
- **Docker** — the `framework/` test suites spin up Postgres via
  testcontainers. Without Docker running, large parts of the suite fail or
  skip. (Alternative: set `TEST_POSTGRES_DSN` and use `make test-pg-env`.)
- **Chrome/Chromium** — the chromedp end-to-end suites (`examples/site`,
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

Anything that prints is a stray binary — move it to `dist/` or delete it.

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

### Known isolation rules

Some suites are reliable alone but flake when sharing a `go test` run:

- `examples/site` chromedp e2e and the axe accessibility gate
  (`TestAxe_AllPagesAreClean`) must run **isolated**, e.g.
  `go test ./examples/site/ -run TestAxe_AllPagesAreClean`.
- `core-ui/runtime` and `core-ui/widget` flake under parallel load — rerun
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
`!` for breaking changes (e.g. `feat(cli)!: …`). One theme per commit — no
mega-commits.

## Where to start reading

- `CLAUDE.md` — repo working agreements (humans benefit too).
- `core-ui/ARCHITECTURE.md` — mandatory before any UI/runtime work.
- `framework/ARCHITECTURE.md` — mandatory before touching `framework/` layout.
- `framework/docs/content/*.md` — the user-facing docs, embedded into the
  `gofastr` binary; docs ship in the same commit as the code they describe.
