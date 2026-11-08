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
	"github.com/DonaldMurillo/gofastr/internal/semver"
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

// validVersion reports whether v (with its leading "v" dropped) is a
// full semver version without build metadata, at most 64 bytes. Semver
// allows only ASCII letters, digits, "." and "-", so the install scripts
// can quote it.
func validVersion(v string) bool {
	return len(v) <= 64 && !strings.ContainsRune(v, '+') && semver.Validate("v"+v) == nil
}

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
	if !validVersion(opts.version) {
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
	// The raw text is what the scripts embed, so the scheme, query and
	// fragment are checked on it: url.Parse lower-cases "HTTPS://" and
	// drops a bare trailing "?" or "#".
	if !strings.HasPrefix(raw, "https://") {
		return "", fmt.Errorf("release: --url %q must be an https:// URL", raw)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("release: --url: %v", err)
	}
	if u.Host == "" {
		return "", fmt.Errorf("release: --url %q must be an https:// URL", raw)
	}
	if u.User != nil || strings.ContainsAny(raw, "?#") {
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

// inspectPackage runs `go list` in dir for every target, in the same
// environment the build uses, so a package that only builds for its
// targets (main_linux.go) can be released from any host. It refuses
// anything but one main package. The service and desktop answers can
// differ by platform: --service needs battery/service on every target,
// and a warning fires when any target imports either.
func inspectPackage(dir, pkg string, targets []target) (pkgInfo, error) {
	var info pkgInfo
	for i, t := range targets {
		ti, err := inspectTarget(dir, pkg, t)
		if err != nil {
			return pkgInfo{}, err
		}
		if i == 0 {
			info = ti
			continue
		}
		info = mergeTargetInfo(info, ti)
	}
	return info, nil
}

// mergeTargetInfo folds one more target's answers into info: the
// package uses battery/service only if every target does, and imports
// it (or battery/desktop) if any target does.
func mergeTargetInfo(info, t pkgInfo) pkgInfo {
	info.usesService = info.usesService && t.usesService
	info.importsService = info.importsService || t.importsService
	info.importsDesktop = info.importsDesktop || t.importsDesktop
	return info
}

func inspectTarget(dir, pkg string, t target) (pkgInfo, error) {
	cmd := exec.Command("go", "list", "-f", "{{.Name}}\t{{.ImportPath}}\t{{join .Imports \" \"}}\t{{join .Deps \" \"}}", "--", pkg)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), buildEnv(t)...)
	cmd.Stderr = os.Stderr
	raw, err := cmd.Output()
	if err != nil {
		return pkgInfo{}, fmt.Errorf("release: go list %s for %s: %v", pkg, t, err)
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

// buildEnv is the environment go list and go build run under for t.
func buildEnv(t target) []string {
	return []string{"CGO_ENABLED=0", "GOOS=" + t.goos, "GOARCH=" + t.goarch}
}

// majorSuffixRe matches the /vN element `go build` skips when it names a
// binary after its import path: v2 and up, with no leading zero. v0 and
// v1 are never a major suffix (cmd/go's isVersionElement).
var majorSuffixRe = regexp.MustCompile(`^v([2-9]|[1-9][0-9]+)$`)

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
