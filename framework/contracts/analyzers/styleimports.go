package analyzers

import (
	"go/build/constraint"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

var stylePlatforms = []stylePlatform{
	{GOOS: "darwin", GOARCH: "amd64"},
	{GOOS: "darwin", GOARCH: "arm64"},
	{GOOS: "linux", GOARCH: "amd64"},
	{GOOS: "linux", GOARCH: "arm64"},
	{GOOS: "windows", GOARCH: "amd64"},
}

type stylePlatform struct {
	GOOS, GOARCH string
}

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

type styleBuildFilter struct {
	never    bool
	expr     constraint.Expr
	fnOS     string
	fnGOARCH string
}

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

type styleModule struct {
	dir  string // the directory holding the go.mod
	path string // the module path it declares
}

type styleModuleResolver struct {
	cache map[string]styleModule
}

func newStyleModuleResolver() *styleModuleResolver {
	return &styleModuleResolver{cache: map[string]styleModule{}}
}

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
