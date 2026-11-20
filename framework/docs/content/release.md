# Releasing a CLI or service binary (`gofastr release`)

`gofastr release` cross-compiles any `package main` (a generated customer CLI, a `battery/service` daemon, or a standalone tool) into reproducible per-OS/CPU archives, a `checksums.txt`, and verified one-line `install.sh` / `install.ps1` installers.

> For desktop GUI apps (`.app` bundles, signing, notarization, WebView2), use `gofastr desktop build` instead (see [Desktop apps](desktop.md)).

## Building and publishing a release

```bash
gofastr release --version=1.2.0 --pkg=./cmd/myapp --include=README.md,LICENSE \
  --url=https://github.com/me/myapp/releases/download/v1.2.0

gh release create v1.2.0 dist/release/1.2.0/*
```

Customers install without Go:

```bash
# macOS / Linux
curl -fsSL https://github.com/me/myapp/releases/latest/download/install.sh | sh
```

```powershell
# Windows
irm https://github.com/me/myapp/releases/latest/download/install.ps1 | iex
```

## Flags and output

Builds with `CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=<version>"` into an empty output directory (default `dist/release/<version>`):

| Flag | Meaning |
| --- | --- |
| `--version` | Required semver (`1.2.0`, `1.2.0-rc.1`; leading `v` stripped; build metadata refused) |
| `--pkg` | Main package path/dir (default `.`) |
| `--name` | Output binary name (defaults to package base name) |
| `-o`, `--out` | Output directory (must be new or empty) |
| `--url` | Plain `https://` release download base URL (required to generate `install.sh` and `install.ps1`) |
| `--targets` | Comma-separated `GOOS/GOARCH` targets (default all 6: darwin/linux/windows × amd64/arm64) |
| `--service` | Install scripts automatically run `<name> service install` after copying the binary |
| `--include` | Extra regular files copied into every archive root (e.g. `README.md,LICENSE`) |
| `--version-var` | Linker `-X` target (default `main.version`) |
| `--ldflags` | Extra linker flags |

Both generated install scripts embed each archive's exact SHA-256 hash, verify before unpacking, install per-user with no root required (`~/.local/bin` or `%LOCALAPPDATA%\Programs\<name>`, overridable via `<NAME>_INSTALL_DIR`), and handle atomic replacement of running binaries.

## Common mistakes

- **Passing a `--url` tag that doesn't match the published GitHub release URL:** every install 404s.
- **Uploading install scripts from one build beside archives from another:** embedded SHA-256 hashes won't match. Always upload the whole output directory together.
- **Forgetting `var version = "dev"` in `main`:** the linker silently ignores `-X` on missing variables.
- **Using `gofastr release` for a desktop GUI app:** use `gofastr desktop build` so macOS `.app` bundles and Windows `WebView2Loader.dll` are packaged properly.
