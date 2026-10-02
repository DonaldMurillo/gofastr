package analyzers

import (
	"go/build/constraint"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

// The machinery the program grouping (ownedstyleprograms.go) needs to
// decide what one binary links: which files build, and which import
// path a directory answers to. Files are judged per target platform —
// build constraints and the _GOOS/_GOARCH filename suffixes included —
// so a main whose imports differ per platform yields per-platform
// programs instead of one union. Directories answer through their
// NEAREST enclosing go.mod, so a package under a nested module (or a
// directory a root go.mod replaces in-tree) imports under the path its
// own module declares, and a pass root with no go.mod still resolves
// against the module that encloses it.

// stylePlatforms is the fixed target list the grouping judges: the
// platforms the framework ships binaries for. Sorted, so program order
// is deterministic.
var stylePlatforms = []stylePlatform{
	{GOOS: "darwin", GOARCH: "amd64"},
	{GOOS: "darwin", GOARCH: "arm64"},
	{GOOS: "linux", GOARCH: "amd64"},
	{GOOS: "linux", GOARCH: "arm64"},
	{GOOS: "windows", GOARCH: "amd64"},
}

// stylePlatform is one GOOS/GOARCH pair a program closure is computed
// for.
type stylePlatform struct {
	GOOS, GOARCH string
}

// The known-GOOS/GOARCH tables go/build matches filename suffixes
// against, and the set the `unix` tag covers (go/build's unixOS). A
// suffix naming no known platform is not a constraint: the go tool
// auto-tags foo_linux.go, never foo_customtag.go.
var (
	styleKnownOS = map[string]bool{
		"aix": true, "android": true, "darwin": true, "dragonfly": true,
		"freebsd": true, "hurd": true, "illumos": true, "ios": true,
		"js": true, "linux": true, "nacl": true, "netbsd": true,
		"openbsd": true, "plan9": true, "solaris": true, "wasip1": true,
		"windows": true, "zos": true,
	}
	styleKnownArch = map[string]bool{
		"386": true, "amd64": true, "amd64p32": true, "arm": true,
		"armbe": true, "arm64": true, "arm64be": true, "loong64": true,
		"mips": true, "mipsle": true, "mips64": true, "mips64le": true,
		"mips64p32": true, "mips64p32le": true, "ppc": true, "ppc64": true,
		"ppc64le": true, "riscv64": true, "riscv": true, "s390": true,
		"s390x": true, "sparc": true, "sparc64": true, "wasm": true,
	}
	styleUnixOS = map[string]bool{
		"aix": true, "android": true, "darwin": true, "dragonfly": true,
		"freebsd": true, "hurd": true, "illumos": true, "ios": true,
		"linux": true, "netbsd": true, "openbsd": true, "solaris": true,
	}
)

// styleTagSatisfied is the per-platform truth of one build tag. GOOS,
// GOARCH and `unix` answer for the platform; `ignore` is never set; a
// tag naming some OTHER platform's GOOS or GOARCH is false. Every other
// tag — gc, go1.N, a project's own — is conservatively TRUE: the
// grouping must not invent a split the project's real builds never
// see. The residual is documented on GOFASTR1816: a custom tag is
// treated as set, so the imports of a `//go:build !extra` file are
// never followed.
func styleTagSatisfied(tag, goos, goarch string) bool {
	switch {
	case tag == goos || tag == goarch:
		return true
	case tag == "unix":
		return styleUnixOS[goos]
	case tag == "ignore":
		return false
	case styleKnownOS[tag], styleKnownArch[tag]:
		return false
	default:
		return true
	}
}

// styleBuildFilter is one Go file's build constraints: the parsed
// //go:build expression (nil when there is none) and the _GOOS/_GOARCH
// filename suffixes ("" when the name carries none). A file whose name
// starts with "." or "_" never builds at all — the go tool's rule.
type styleBuildFilter struct {
	never    bool
	expr     constraint.Expr
	fnOS     string
	fnGOARCH string
}

// newStyleBuildFilter reads a file's constraints from its name and its
// bytes. A malformed //go:build line makes the file never build: the
// compiler refuses it, so no binary links it.
func newStyleBuildFilter(rel, src string) styleBuildFilter {
	name := path.Base(rel)
	if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
		return styleBuildFilter{never: true}
	}
	expr, malformed := styleBuildExpr(src)
	if malformed {
		return styleBuildFilter{never: true}
	}
	goos, goarch := styleFileSuffix(name)
	return styleBuildFilter{expr: expr, fnOS: goos, fnGOARCH: goarch}
}

// buildsOn reports whether the file compiles into a binary for p.
func (f styleBuildFilter) buildsOn(p stylePlatform) bool {
	if f.never {
		return false
	}
	if f.fnOS != "" && f.fnOS != p.GOOS {
		return false
	}
	if f.fnGOARCH != "" && f.fnGOARCH != p.GOARCH {
		return false
	}
	return f.expr == nil || f.expr.Eval(func(tag string) bool {
		return styleTagSatisfied(tag, p.GOOS, p.GOARCH)
	})
}

// styleBuildExpr parses the file's leading //go:build (or legacy
// // +build) constraint. Only the header counts — a directive below
// the package clause is a comment — and the first //go:build line wins.
// The second return value reports a constraint that is present but does
// not parse: the go tool refuses the file, so the caller treats it as
// never building rather than always.
func styleBuildExpr(src string) (constraint.Expr, bool) {
	var plus constraint.Expr
	for _, raw := range strings.Split(src, "\n") {
		line := strings.TrimRight(raw, "\r")
		if strings.HasPrefix(line, "package ") || strings.HasPrefix(line, "package\t") {
			break
		}
		if constraint.IsGoBuild(line) {
			expr, err := constraint.Parse(line)
			if err != nil {
				return nil, true
			}
			return expr, false
		}
		if constraint.IsPlusBuild(line) {
			if expr, err := constraint.Parse(line); err == nil {
				if plus == nil {
					plus = expr
				} else {
					plus = &constraint.AndExpr{X: plus, Y: expr}
				}
			}
		}
	}
	return plus, false
}

// styleFileSuffix reads the _GOOS/_GOARCH suffix constraint from a file
// name, the go tool's goodOSArchFile rule: after the first "_", a final
// GOOS, GOARCH, or GOOS_GOARCH pair (in that order) constrains the
// file; anything else (foo_customtag.go) does not.
func styleFileSuffix(name string) (goos, goarch string) {
	name, _, _ = strings.Cut(name, ".")
	i := strings.Index(name, "_")
	if i < 0 {
		return "", ""
	}
	parts := strings.Split(name[i:], "_")
	if n := len(parts); n > 0 && parts[n-1] == "test" {
		parts = parts[:n-1]
	}
	n := len(parts)
	if n >= 2 && styleKnownOS[parts[n-2]] && styleKnownArch[parts[n-1]] {
		return parts[n-2], parts[n-1]
	}
	if n >= 1 && styleKnownOS[parts[n-1]] {
		return parts[n-1], ""
	}
	if n >= 1 && styleKnownArch[parts[n-1]] {
		return "", parts[n-1]
	}
	return "", ""
}

// styleModule is the nearest go.mod enclosing a directory. The zero
// value means none was found.
type styleModule struct {
	dir  string // the directory holding the go.mod
	path string // the module path it declares
}

// styleModuleResolver resolves directories to import paths through
// their nearest enclosing go.mod. The walk starts at each queried
// directory and continues past the pass root when the root itself has
// no go.mod (a go.work root, or a tree verified from a subdirectory),
// so packages of the enclosing module still resolve. Results are
// memoized per directory along every walked chain.
type styleModuleResolver struct {
	cache map[string]styleModule
}

func newStyleModuleResolver() *styleModuleResolver {
	return &styleModuleResolver{cache: map[string]styleModule{}}
}

// moduleFor returns the nearest module enclosing dir, walking to the
// filesystem root when no go.mod sits above dir at all.
func (r *styleModuleResolver) moduleFor(dir string) styleModule {
	if m, ok := r.cache[dir]; ok {
		return m
	}
	var mod styleModule
	for d := dir; ; {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			mod = styleModule{dir: d, path: contracts.ReadModulePath(d)}
			break
		}
		parent := filepath.Dir(d)
		if parent == d {
			break // filesystem root, no go.mod anywhere above
		}
		d = parent
	}
	// Every directory on the walked chain shares the answer; the walk
	// stops at the module's own directory, whose parent belongs to the
	// next module out.
	for d := dir; ; {
		r.cache[d] = mod
		if mod.dir != "" && d == mod.dir {
			break
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}
	return mod
}

// importPathFor returns dir's import path: the nearest module's path
// plus the slash path from that module's directory. "" when no go.mod
// encloses the directory or the module declares no path — the
// directory's packages then answer to no import the grouping can
// resolve.
func (r *styleModuleResolver) importPathFor(dir string) string {
	mod := r.moduleFor(dir)
	if mod.dir == "" || mod.path == "" {
		return ""
	}
	if dir == mod.dir {
		return mod.path
	}
	rel, err := filepath.Rel(mod.dir, dir)
	if err != nil {
		return ""
	}
	return mod.path + "/" + filepath.ToSlash(rel)
}
