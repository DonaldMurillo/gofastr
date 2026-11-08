# Releasing a CLI or service binary

`gofastr release` turns a main package into a set of downloads: one
archive per OS and CPU, a `checksums.txt`, and two install scripts. A
customer with no Go toolchain installs the binary with one line:

```bash
curl -fsSL https://github.com/me/myapp/releases/download/v1.2.0/install.sh | sh
```

```powershell
irm https://github.com/me/myapp/releases/download/v1.2.0/install.ps1 | iex
```

It is meant for the binaries that are not window apps: a CLI made by
`gofastr generate cli` ([app-cli](app-cli.md)), a
[service](service.md) app, or any other `package main`. A
[desktop](desktop.md) app is built, signed and bundled with
`gofastr desktop build` instead.

## Building a release

```bash
gofastr release --version=1.2.0 --pkg=./cmd/myapp \
  --url=https://github.com/me/myapp/releases/download/v1.2.0
```

Each target is built with `CGO_ENABLED=0 go build -trimpath` and the
linker flags `-s -w -X main.version=<version>`, so a package with
`var version = "dev"` in `main` reports the release it was built as.
The CLI `gofastr generate cli` writes has that variable. `--version-var`
names another variable, and the linker ignores `-X` for a variable that
does not exist. The package is listed and built with each target's
`GOOS` and `GOARCH`, so a main package whose files only build for Linux
releases from a Mac.

The output directory (default `dist/release/<version>`) must be new or
empty, so archives from an older build never sit beside a newer
`checksums.txt`. It ends up holding:

| File | Contents |
|------|----------|
| `<name>_<version>_<os>_<arch>.tar.gz` | macOS and Linux builds: the binary at the archive root, mode 0755, plus any `--include` files |
| `<name>_<version>_windows_<arch>.zip` | Windows builds: `<name>.exe` plus any `--include` files |
| `install.sh` | macOS and Linux installer (written with `--url`) |
| `install.ps1` | Windows installer (written with `--url`) |
| `checksums.txt` | SHA-256 of every archive and script, in the `sha256sum` format, so `sha256sum -c checksums.txt` checks a downloaded set |

The archives carry no timestamps or owners and list their entries in a
fixed order, so two releases of the same tree are byte-identical.

### Flags

| Flag | Meaning |
|------|---------|
| `--version` | Required. Semver such as `1.2.0` or `1.2.0-rc.1`; a leading `v` is dropped from file names and from the stamped value. Build metadata (`+…`) is refused |
| `--pkg` | The main package (default `.`). Anything else is refused |
| `--name` | The binary name. Default: the name `go build` gives the package (the last import path element, skipping a `/vN` suffix). It must start with a letter and hold only letters, digits, `.`, `_` and `-`. When Windows is a target, a name Windows reserves (`con`, `aux`, `nul`, `com1`…) is refused |
| `-o`, `--out` | Output directory (default `dist/release/<version>`) |
| `--url` | The https URL the files will be downloaded from. Without it no install scripts are written |
| `--targets` | Comma-separated `GOOS/GOARCH` pairs. Default and maximum: `darwin/amd64`, `darwin/arm64`, `linux/amd64`, `linux/arm64`, `windows/amd64`, `windows/arm64` |
| `--service` | The app uses `battery/service`: the scripts run `<name> service install` once the binary is in place. Refused unless the package uses it on every target |
| `--include` | Comma-separated files added to every archive at its root, for example `README.md,LICENSE`. Each must be a regular file (a symlink is refused) whose name is ASCII letters, digits, `.`, `_`, `+` and `-`. Names must differ from each other and from the binary ignoring case, because macOS and Windows unpack onto case-insensitive file systems: `--include=HELLO` would overwrite `hello` |
| `--version-var` | The variable `-X` sets (default `main.version`) |
| `--ldflags` | Extra linker flags, appended to the defaults |

`--url` is refused unless it is a plain https URL made of ASCII letters,
digits and URL punctuation with no quote, `$` or backtick, because it is
written into both scripts inside quoted strings. Percent-encode anything
else. A query string, a fragment or credentials in the URL are refused
too.

## What the install scripts do

Both scripts are generated for one release and carry the SHA-256 of each
archive in their own text. They do not download `checksums.txt`: the
script is what the user chose to run, so it is where the expected hashes
come from. A download whose hash differs is refused before anything is
unpacked, and nothing is installed.

`install.sh` (macOS and Linux, any POSIX `sh`, BusyBox included):

1. Picks the build from `uname -s` and `uname -m`. On Apple silicon, a
   shell running under Rosetta reports `x86_64`; the script installs the
   arm64 build when the release has one.
2. Downloads it with `curl` (https only, redirects included) or `wget`,
   into a `mktemp -d` directory it removes on exit.
3. Checks the hash with `sha256sum`, `shasum -a 256` or `openssl`,
   whichever exists.
4. Copies the binary into `~/.local/bin` (or `$<NAME>_INSTALL_DIR`)
   under a temporary name and renames it over the old one. The rename is
   atomic and works while an old copy is running.
5. Prints the `export PATH=…` line to add when the directory is not on
   `PATH`. It never edits a shell profile.
6. With `--service`, runs `<name> service install`, which copies the
   binary to its managed path and registers the service
   ([service](service.md#verbs)). `<NAME>_NO_SERVICE=1` skips this step.

`install.ps1` (Windows 10 and later, Windows PowerShell 5.1 or
PowerShell 7):

1. Picks the build from the OS architecture (x64 or Arm64), falling back
   to `PROCESSOR_ARCHITEW6432` and `PROCESSOR_ARCHITECTURE` where .NET
   does not report it.
2. Downloads it with `Invoke-WebRequest`, with TLS 1.2 turned on for
   Windows PowerShell 5.1, and checks it with `Get-FileHash`.
3. Installs `<name>.exe` into `%LOCALAPPDATA%\Programs\<name>` (or
   `$env:<NAME>_INSTALL_DIR`). A running `.exe` cannot be overwritten but
   can be renamed, so the old copy is moved aside first and put back if
   the copy fails.
4. Adds the directory to the user's `Path` in the registry. It reads and
   writes the value unexpanded, as `REG_EXPAND_SZ`, so entries like
   `%USERPROFILE%\bin` survive, and it announces the change so terminals
   opened afterwards see it.
5. With `--service`, runs `<name> service install`.

Neither script needs root or an administrator. `<NAME>` is the binary
name in upper case with every other character turned into `_`
(`my-app` → `MY_APP`), the same prefix a generated CLI uses for
`MY_APP_URL`.

## Publishing on GitHub

Upload the whole output directory to the release the URL names:

```bash
gofastr release --version=1.2.0 --pkg=./cmd/myapp --include=README.md,LICENSE \
  --url=https://github.com/me/myapp/releases/download/v1.2.0
gh release create v1.2.0 dist/release/1.2.0/*
```

GitHub also serves the newest release's files under
`releases/latest/download/`, so
`curl -fsSL https://github.com/me/myapp/releases/latest/download/install.sh | sh`
always installs the latest version: each release's script installs the
version it was built for.

Any static host works the same way: put the files in one directory and
pass its URL.

## Upgrading and uninstalling

Running a newer release's script replaces the binary in place. For a
service app, `service install` stops the running copy, swaps the binary
and starts the new one. To uninstall, delete the binary (and run
`<name> service uninstall` first for a service app). The scripts keep no
record of what they installed.

## What it does not do

- **No code signing** beyond the ad-hoc signature the Go linker puts on
  every darwin/arm64 binary. `curl` sets no quarantine flag, so macOS
  Gatekeeper does not block a binary `install.sh` installed. Sign the
  binaries before you archive them if your customers need signed ones.
- **No package managers.** There is no Homebrew formula, Scoop manifest
  or `.deb`/`.rpm` yet.
- **No installer wizard.** The scripts are the terminal path; a
  double-click installer is separate work.

## What was tested

Both scripts run end to end in the test suite against a local HTTPS
server: install, a second install over the first, and a tampered
archive that must be refused with nothing installed. `install.sh` runs
on macOS and Linux, and was also run by hand on arm64 Alpine, under
BusyBox `sh` and `wget`. `install.ps1` runs on the GitHub Windows runner under
Windows PowerShell 5.1, the user `Path` edit included. PowerShell 7
and Arm64 Windows have not been run.

## Common mistakes

- **A `--url` that is not where the files end up.** The scripts fetch
  archives from exactly that directory. Building with
  `.../download/v1.2.0` and publishing the tag `1.2.0` makes every
  install a 404.
- **Uploading the scripts without the archives, or from another build.**
  The hashes in a script match only the archives built beside it. Upload
  the whole output directory together, and rebuild everything rather
  than swapping one archive.
- **A version variable `-X` cannot set.** The linker ignores `-X` for a
  name that does not exist or is not a string variable, so the binary
  prints `dev` with no error. Declare `var version = "dev"` in `main`,
  or point `--version-var` at the real one.
- **Releasing a desktop app with it.** The archives hold the plain
  binary: no `.app` bundle, no signing, no WebView2 loader. Use
  `gofastr desktop build`.
