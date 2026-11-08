package releasecmd

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/project"
)

// target is one GOOS/GOARCH pair a release builds.
type target struct{ goos, goarch string }

func (t target) String() string { return t.goos + "/" + t.goarch }

// supportedTargets are the platforms the install scripts know how to
// detect. Windows means Windows 10 and later; the scripts do not run on
// anything older.
var supportedTargets = []target{
	{"darwin", "amd64"}, {"darwin", "arm64"},
	{"linux", "amd64"}, {"linux", "arm64"},
	{"windows", "amd64"}, {"windows", "arm64"},
}

type options struct {
	version    string // without a leading "v"
	pkg        string
	name       string
	out        string
	baseURL    string // "" writes no install scripts
	ldflags    string
	versionVar string
	targets    []target
	includes   []string
	service    bool
}

// nameRe is a binary name the scripts can quote and derive an
// environment-variable prefix from: ASCII, starting with a letter.
var nameRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._-]{0,63}$`)

// versionRe is semver without build metadata; a leading "v" is dropped
// before the match.
var versionRe = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z]+(\.[0-9A-Za-z-]+)*)?$`)

// urlCharsRe is the set of characters a base URL may hold. It leaves out
// every character that means something inside a single-quoted sh or
// PowerShell string ('), or that a shell or PowerShell would expand
// ($, `), along with whitespace, quotes and anything outside ASCII
// (PowerShell also treats the curly quotes as quote characters).
var urlCharsRe = regexp.MustCompile(`^[A-Za-z0-9._~:/?#\[\]@!&()*+,;=%-]+$`)

// versionVarRe is a Go linker -X symbol: an import path, a dot, an
// identifier.
var versionVarRe = regexp.MustCompile(`^[A-Za-z0-9._/~-]+\.[A-Za-z_][A-Za-z0-9_]*$`)

func parseOptions(args []string) (options, error) {
	var version, pkg, name, out, base, ldflags, versionVar, targets, include string
	var service bool
	pkg, versionVar = ".", "main.version"
	seen, err := cli.ParseFlags(args, map[string]*string{
		"--version":     &version,
		"--pkg":         &pkg,
		"--name":        &name,
		"-o":            &out,
		"--out":         &out,
		"--url":         &base,
		"--ldflags":     &ldflags,
		"--version-var": &versionVar,
		"--targets":     &targets,
		"--include":     &include,
	}, map[string]*bool{"--service": &service})
	if err != nil {
		return options{}, fmt.Errorf("release: %w", err)
	}
	if !seen["--version"] {
		return options{}, errors.New("release: --version is required (for example --version=1.2.0)")
	}
	opts := options{
		version:    strings.TrimPrefix(version, "v"),
		pkg:        pkg,
		name:       name,
		ldflags:    ldflags,
		versionVar: versionVar,
		service:    service,
	}
	if !versionRe.MatchString(opts.version) || len(opts.version) > 64 {
		return options{}, fmt.Errorf("release: --version %q must be semver, like 1.2.0 or 1.2.0-rc.1 (a leading v is allowed)", version)
	}
	if opts.name != "" && !nameRe.MatchString(opts.name) {
		return options{}, fmt.Errorf("release: --name %q must start with a letter and hold only letters, digits, '.', '_' and '-' (at most 64)", opts.name)
	}
	if !versionVarRe.MatchString(opts.versionVar) {
		return options{}, fmt.Errorf("release: --version-var %q must name a Go string variable, like main.version", opts.versionVar)
	}
	if base != "" {
		if opts.baseURL, err = checkBaseURL(base); err != nil {
			return options{}, err
		}
	}
	if opts.targets, err = parseTargets(targets); err != nil {
		return options{}, err
	}
	if include != "" {
		for _, f := range strings.Split(include, ",") {
			if f = strings.TrimSpace(f); f == "" {
				return options{}, errors.New("release: --include has an empty entry")
			}
			opts.includes = append(opts.includes, f)
		}
	}
	opts.out = out
	if opts.out == "" {
		opts.out = filepath.Join("dist", "release", opts.version)
	}
	return opts, nil
}

// checkBaseURL validates the download location the install scripts
// fetch archives from and returns it without a trailing slash.
func checkBaseURL(raw string) (string, error) {
	if !urlCharsRe.MatchString(raw) {
		return "", fmt.Errorf("release: --url %q holds a character the install scripts cannot quote (quotes, $, `, spaces and non-ASCII are refused; percent-encode them)", raw)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("release: --url: %v", err)
	}
	if u.Scheme != "https" || u.Host == "" {
		return "", fmt.Errorf("release: --url %q must be an https:// URL", raw)
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("release: --url %q must be a plain directory URL, with no credentials, query or fragment", raw)
	}
	return strings.TrimRight(raw, "/"), nil
}

func parseTargets(s string) ([]target, error) {
	if s == "" {
		return slices.Clone(supportedTargets), nil
	}
	var out []target
	for _, raw := range strings.Split(s, ",") {
		raw = strings.TrimSpace(raw)
		goos, goarch, ok := strings.Cut(raw, "/")
		t := target{goos, goarch}
		if !ok || !slices.Contains(supportedTargets, t) {
			return nil, fmt.Errorf("release: --targets entry %q is not one of %s", raw, targetList())
		}
		if slices.Contains(out, t) {
			return nil, fmt.Errorf("release: --targets names %s twice", raw)
		}
		out = append(out, t)
	}
	return out, nil
}

func targetList() string {
	names := make([]string, len(supportedTargets))
	for i, t := range supportedTargets {
		names[i] = t.String()
	}
	return strings.Join(names, ", ")
}

// pkgInfo is what `go list` reports about the package being released.
type pkgInfo struct {
	importPath string
	// usesService is battery/service anywhere in the build: --service
	// is refused without it.
	usesService bool
	// importsService and importsDesktop are direct imports of the main
	// package, the shape of a service or desktop app's main.go; they
	// only decide the warnings, so a CLI that pulls either in through a
	// library is not told it is one.
	importsService bool
	importsDesktop bool
}

const (
	servicePkg = project.GofastrModule + "/battery/service"
	desktopPkg = project.GofastrModule + "/battery/desktop"
)

// inspectPackage runs `go list` in dir and refuses anything but one main
// package.
func inspectPackage(dir, pkg string) (pkgInfo, error) {
	cmd := exec.Command("go", "list", "-f", "{{.Name}}\t{{.ImportPath}}\t{{join .Imports \" \"}}\t{{join .Deps \" \"}}", "--", pkg)
	cmd.Dir = dir
	cmd.Stderr = os.Stderr
	raw, err := cmd.Output()
	if err != nil {
		return pkgInfo{}, fmt.Errorf("release: go list %s: %v", pkg, err)
	}
	line := strings.TrimSuffix(string(raw), "\n")
	fields := strings.Split(line, "\t")
	if strings.Contains(line, "\n") || len(fields) != 4 {
		return pkgInfo{}, fmt.Errorf("release: --pkg %s must name one package", pkg)
	}
	if fields[0] != "main" {
		return pkgInfo{}, fmt.Errorf("release: %s is package %s, not a main package", fields[1], fields[0])
	}
	imports, deps := strings.Fields(fields[2]), strings.Fields(fields[3])
	return pkgInfo{
		importPath:     fields[1],
		usesService:    slices.Contains(deps, servicePkg),
		importsService: slices.Contains(imports, servicePkg),
		importsDesktop: slices.Contains(imports, desktopPkg),
	}, nil
}

// majorSuffixRe matches the /vN element `go build` skips when it names a
// binary after its import path.
var majorSuffixRe = regexp.MustCompile(`^v[0-9]+$`)

// defaultName is the binary name `go build` gives the package.
func defaultName(importPath string) string {
	base := path.Base(importPath)
	if majorSuffixRe.MatchString(base) && path.Dir(importPath) != "." {
		base = path.Base(path.Dir(importPath))
	}
	return base
}

// RunRelease is `gofastr release`.
func RunRelease(args []string) {
	if cli.HasHelpFlag(args) {
		printUsage()
		return
	}
	opts, err := parseOptions(args)
	if err != nil {
		cli.Fail("%v", err)
		cli.OsExit(1)
		return
	}
	if err := project.ValidateBuildTarget(opts.pkg); err != nil {
		cli.Fail("Release target %q is invalid: %v", opts.pkg, err)
		cli.OsExit(1)
		return
	}
	wd, err := os.Getwd()
	if err != nil {
		cli.Fail("release: %v", err)
		cli.OsExit(1)
		return
	}
	res, err := release(wd, opts, os.Stdout)
	if err != nil {
		cli.Fail("%v", err)
		cli.OsExit(1)
		return
	}
	printSummary(res, opts)
}
