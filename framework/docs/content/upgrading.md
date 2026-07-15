# Upgrading — move an app to a newer GoFastr release

Two things version independently: the **Go module dependency** your app
builds against, and the **`gofastr` CLI** installed with `go install`.
Updating `go.mod` does NOT update the CLI binary — keep them on the same
release. Every command below uses `vX.Y.Z` as a placeholder; substitute
the release you're upgrading to.

## The guided path: `gofastr upgrade`

The CLI embeds a migration registry — one entry per release that
carried breaking or migration-relevant changes, maintained alongside
the changelog. `gofastr upgrade` reads your `go.mod`, resolves the
target release, and shows exactly what you'll cross on the way:

```bash
# Install the TARGET release's CLI first — an older binary's registry
# can't know about releases that came after it:
go install github.com/DonaldMurillo/gofastr/cmd/gofastr@vX.Y.Z

gofastr upgrade                # plan against the newest tagged release
gofastr upgrade --to vX.Y.Z    # plan against a specific release
gofastr upgrade --apply        # also run go get / tidy / build / test
```

The plan lists each in-between release's notes (BREAKING entries
first-class), and where a change has a known code signature, the report
points at the affected `file:line` in *your* project — the same guided
style as `gofastr audit a11y`. `--apply` then runs the mechanical
steps and stops at the first failure so you fix with the notes in hand.

## The manual workflow

1. **List the available releases:**

   ```bash
   go list -m -versions github.com/DonaldMurillo/gofastr
   ```

2. **Read the release notes** for the release you chose — breaking
   changes, migrations, and deprecations are called out per release:
   <https://github.com/DonaldMurillo/gofastr/releases> (the same
   content ships in `CHANGELOG.md`). Do this *before* touching
   `go.mod`.

3. **Update the module dependency:**

   ```bash
   go get github.com/DonaldMurillo/gofastr@vX.Y.Z
   ```

4. **Update the standalone CLI to the same release** (if you use it):

   ```bash
   go install github.com/DonaldMurillo/gofastr/cmd/gofastr@vX.Y.Z
   ```

5. **Clean the module files:**

   ```bash
   go mod tidy
   ```

6. **Build and test:**

   ```bash
   go build ./...
   go test ./...
   ```

7. **Confirm which version Go actually selected** (MVS can pick a
   higher version than you asked for if another dependency requires it):

   ```bash
   go list -m github.com/DonaldMurillo/gofastr
   ```

8. **Review the `go.mod` / `go.sum` diff before committing.**

For the CLI, `gofastr --help` is the smoke test that the new binary is
on your PATH. Don't treat `gofastr version` as proof of the installed
tag — `go install` builds don't always carry injected version metadata.

## Common mistakes

- **Upgrading `go.mod` but not the CLI.** `go get` changes what your
  app builds against; the `gofastr` binary on your PATH stays whatever
  you last installed. Generators, `gofastr build`'s gates, and the
  embedded docs then disagree with your framework version. Step 4 is
  not optional if you use the CLI.
- **Running `gofastr upgrade` with the OLD binary.** The migration
  registry ships inside the CLI, so a binary older than the target
  can't know the target's notes (it warns when this happens). Install
  the target CLI first, then plan the upgrade.
- **Skipping the release notes on a multi-release jump.** Breaking
  changes are per-release; jumping vA→vD means reading B, C, and D.
  `gofastr upgrade` collects all of them for exactly this case.
- **Trusting `go get` over `go list -m`.** Another dependency can pull
  a newer GoFastr than you requested (minimal version selection).
  Step 7 tells you what you're actually running.
- **Committing `go.sum` churn blind.** The diff should contain the
  gofastr bump and its transitive updates — anything else deserves a
  look before it lands.
