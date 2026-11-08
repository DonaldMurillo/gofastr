package releasecmd

import (
	"fmt"
	"slices"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
)

func printUsage() {
	fmt.Print(`
gofastr release: cross-build a CLI or service binary and write its install scripts

Usage:
  gofastr release --version=<semver> [flags]

Builds the main package for every target with CGO_ENABLED=0, packs each
build as <name>_<version>_<os>_<arch>.tar.gz (.zip on Windows), and writes
checksums.txt. With --url it also writes install.sh (macOS, Linux) and
install.ps1 (Windows 10 and later), which download the archive for the
machine they run on, check its SHA-256 against the value baked into the
script, and install the binary for the current user. Upload the whole
output directory to --url.

Flags:
  --version=<semver>   Release version, like 1.2.0 or v1.2.0-rc.1 (required)
  --pkg=<path>         Main package to build (default .)
  --name=<binary>      Binary name (default: what go build would name it)
  -o, --out=<dir>      Output directory, created or empty (default dist/release/<version>)
  --url=<https-url>    Where the files will be downloaded from; writes the install scripts
  --targets=<list>     Comma-separated GOOS/GOARCH pairs (default: all of
                       darwin/amd64, darwin/arm64, linux/amd64, linux/arm64,
                       windows/amd64, windows/arm64)
  --service            The binary uses battery/service: the scripts run
                       "<name> service install" after installing it
  --include=<files>    Comma-separated files added to every archive (README, LICENSE)
  --version-var=<sym>  Variable set to the version with -X (default main.version)
  --ldflags=<flags>    Extra linker flags, appended to "-s -w -X <version-var>=<version>"

Examples:
  gofastr release --version=1.2.0 --pkg=./cmd/myapp \
    --url=https://github.com/me/myapp/releases/download/v1.2.0
  gofastr release --version=1.2.0 --service --targets=linux/amd64,linux/arm64

Docs: gofastr docs release
`)
}

func printSummary(res result, opts options) {
	for _, w := range res.warnings {
		cli.Warn("%s", w)
	}
	cli.Success("Released %s %s into %s", res.name, opts.version, res.out)
	for _, a := range res.artifacts {
		cli.Printf("    %s\n", a.file)
	}
	for _, s := range res.scripts {
		cli.Printf("    %s\n", s)
	}
	cli.Printf("    checksums.txt\n")
	if opts.baseURL == "" {
		cli.Info("No --url, so no install scripts were written")
		return
	}
	cli.Printf("\n  Upload every file to %s, then install with:\n", opts.baseURL)
	if slices.Contains(res.scripts, "install.sh") {
		cli.Printf("    curl -fsSL %s/install.sh | sh\n", opts.baseURL)
	}
	if slices.Contains(res.scripts, "install.ps1") {
		cli.Printf("    irm %s/install.ps1 | iex\n", opts.baseURL)
	}
}
