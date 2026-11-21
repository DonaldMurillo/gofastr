# Upgrading GoFastr

Your app's **Go module dependency** (`go.mod`) and the **`gofastr` CLI binary** version independently — keep both on the same release when upgrading.

> Crossing `v0.87.0`? Read [What changed in v0.87.0](release-0-87.md) first. Crossing `v0.86.0`? Read [What changed in v0.86.0](release-0-86.md) first.

## Guided upgrades (`gofastr upgrade`)

The CLI embeds a release-by-release migration registry that scans your codebase, YAML configs, stylesheets, and HTML attributes for breaking changes and prints exact `file:line` fix instructions:

```bash
# 1. Install the TARGET release's CLI first (older CLIs don't know newer notes):
go install github.com/DonaldMurillo/gofastr/cmd/gofastr@vX.Y.Z

# 2. Plan the upgrade against your current go.mod:
gofastr upgrade                 # newest tagged release
gofastr upgrade --to vX.Y.Z     # specific target release
gofastr upgrade --apply         # also run go get / tidy / vendor / build / test
gofastr upgrade --from vA.B.C   # if go.mod was already bumped before scanning
```

Notes on `gofastr upgrade`:
- Exits `1` while any **BREAKING** edit note still matches your code (`2` for a refused downgrade without `--allow-downgrade`).
- Lists any unreadable, oversized (>8 MiB), symlink-escaping, or complex build-tag files under **Unscanned** so you can review them manually.
- Reports any [unmounted](unmount.md) framework packages with links to their upstream diffs (never overwrites unmounted copies).

## Manual upgrade checklist

1. List releases: `go list -m -versions github.com/DonaldMurillo/gofastr`
2. Read `CHANGELOG.md` / release notes for every release between your current and target versions.
3. Update both module and CLI:
   ```bash
   go get github.com/DonaldMurillo/gofastr@vX.Y.Z
   go install github.com/DonaldMurillo/gofastr/cmd/gofastr@vX.Y.Z
   go mod tidy
   ```
4. Verify the resolved version (MVS may select a higher version): `go list -m github.com/DonaldMurillo/gofastr`
5. Build, test, and review `go.mod` / `go.sum`: `go build ./... && go test ./...`

## Common mistakes

- **Running `go get` without updating the `gofastr` CLI:** generators, `gofastr build` gates, and embedded docs fall out of sync with your module version.
- **Running `gofastr upgrade` with the old CLI binary:** install the target CLI first so it carries the target release's migration notes.
- **Bumping `go.mod` before running `gofastr upgrade` without `--from`:** `gofastr upgrade` sees you're already on the target version and skips intermediate notes. Pass `--from vOld`.
- **Ignoring the **Unscanned** list:** review unscanned files before assuming zero matches means a clean migration.
