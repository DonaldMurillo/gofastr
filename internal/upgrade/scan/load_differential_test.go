package scan

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// differentialFixtures is every build-constraint rule the solver claims
// to implement, plus the go/build rules it must not miss. Each entry is
// a file name (its _GOOS, _GOARCH or _GOOS_GOARCH suffix may constrain,
// _test included) and a source (its //go:build line may constrain).
// No verdict is hard-coded: the go command decides.
var differentialFixtures = []struct{ name, src string }{
	// Plain OS tags.
	{"x.go", "//go:build linux\n\npackage fx\n"},
	{"x.go", "//go:build windows\n\npackage fx\n"},
	{"x.go", "//go:build darwin\n\npackage fx\n"},
	{"x.go", "//go:build !windows\n\npackage fx\n"},
	{"x.go", "//go:build linux || darwin\n\npackage fx\n"},
	{"x.go", "//go:build linux && amd64\n\npackage fx\n"},
	{"x.go", "//go:build !(linux || darwin)\n\npackage fx\n"},
	// Single-arch operating systems: the pair, not just the OS, must be
	// one the go command runs.
	{"x.go", "//go:build solaris\n\npackage fx\n"},
	{"x.go", "//go:build illumos\n\npackage fx\n"},
	{"x.go", "//go:build aix\n\npackage fx\n"},
	// A known GOOS no supported pair reaches.
	{"x.go", "//go:build zos\n\npackage fx\n"},
	// Implied OS tags, both directions: GOOS=android matches linux tags
	// and files, ios matches darwin, illumos matches solaris; and a
	// constraint naming the implying OS needs the GOOS that implies it.
	{"x_android.go", "//go:build linux\n\npackage fx\n"},
	{"x_ios.go", "//go:build darwin\n\npackage fx\n"},
	{"x_illumos.go", "//go:build solaris\n\npackage fx\n"},
	{"x_darwin.go", "//go:build ios\n\npackage fx\n"},
	{"x_linux.go", "//go:build android\n\npackage fx\n"},
	// The unix shorthand and its negation.
	{"x.go", "//go:build unix\n\npackage fx\n"},
	{"x.go", "//go:build !unix\n\npackage fx\n"},
	// cgo and the compiler tags.
	{"x.go", "//go:build cgo\n\npackage fx\n"},
	{"x.go", "//go:build !cgo\n\npackage fx\n"},
	{"x.go", "//go:build gc\n\npackage fx\n"},
	{"x.go", "//go:build gccgo\n\npackage fx\n"},
	// go/build remaps boringcrypto to goexperiment.boringcrypto, which
	// no -tags value satisfies.
	{"x.go", "//go:build boringcrypto\n\npackage fx\n"},
	// Free tags.
	{"x.go", "//go:build purego\n\npackage fx\n"},
	{"x.go", "//go:build mytag\n\npackage fx\n"},
	{"x.go", "//go:build !mytag\n\npackage fx\n"},
	{"x.go", "//go:build mytag && other\n\npackage fx\n"},
	{"x.go", "//go:build mytag && linux\n\npackage fx\n"},
	// Filename suffixes.
	{"x_linux.go", "package fx\n"},
	{"x_windows_amd64.go", "package fx\n"},
	{"x_amd64.go", "package fx\n"},
	{"x_wasm.go", "package fx\n"},
	{"x_js_wasm.go", "package fx\n"},
	{"x_wasip1_wasm.go", "package fx\n"},
	{"x_zos.go", "package fx\n"},
	{"x_linux_test.go", "package fx\n"},
	{"x_test.go", "//go:build mytag\n\npackage fx\n"},
	// Release tags.
	{"x.go", "//go:build go1.20\n\npackage fx\n"},
	{"x.go", "//go:build !go1.99\n\npackage fx\n"},
	// A constraint the filename contradicts: no configuration exists.
	{"x_linux.go", "//go:build windows\n\npackage fx\n"},
	// An arch the host may lack, each way round.
	{"x.go", "//go:build arm64\n\npackage fx\n"},
	{"x.go", "//go:build amd64\n\npackage fx\n"},
}

// TestDifferentialSolverMatchesGoList checks every configuration the
// solver picks against the go command itself. For a satisfiable file,
// go list run with the solver's GOOS, GOARCH, tags and CGO_ENABLED must
// compile the file (GoFiles, or the test lists for a _test.go file),
// not ignore it. For a file the solver reports unsatisfiable, the host
// configuration plus one per platform tag the constraint names must
// each leave the file ignored.
func TestDifferentialSolverMatchesGoList(t *testing.T) {
	if testing.Short() {
		t.Skip("runs one go list per distinct build configuration")
	}
	testEnv(t)
	root := t.TempDir()
	writeWorkspaceFile(t, root, "go.mod", "module example.com/fx\n\ngo 1.27.0\n")

	type claim struct {
		label     string
		impPath   string
		file      string
		cfg       buildConfig
		wantBuilt bool
	}
	var claims []claim
	for i, f := range differentialFixtures {
		dir := fmt.Sprintf("f%02d", i)
		// The anchor keeps the package listable whatever the fixture
		// excludes; the assertion only ever names the fixture file.
		writeWorkspaceFile(t, root, dir+"/a.go", "package fx\n")
		writeWorkspaceFile(t, root, dir+"/"+f.name, f.src)
		label := fixtureLabel(f.name, f.src)
		impPath := "example.com/fx/" + dir
		cfg, ok := solveFileConfig([]byte(f.src), f.name)
		if ok {
			claims = append(claims, claim{label, impPath, f.name, cfg, true})
			continue
		}
		// Unsatisfiable per the solver: every cheap probe must agree.
		// The filename's own platform is probed too: an implication the
		// solver lost (say ios implies darwin) shows up exactly here,
		// a file it calls unsatisfiable that builds under its suffix.
		probes := []buildConfig{{goos: runtime.GOOS, goarch: runtime.GOARCH}}
		if expr, has := fileBuildConstraint([]byte(f.src)); has {
			constraintTags(expr, func(tag string) {
				switch {
				case knownOS[tag]:
					probes = append(probes, buildConfig{goos: tag, goarch: runtime.GOARCH})
				case knownArch[tag]:
					probes = append(probes, buildConfig{goos: runtime.GOOS, goarch: tag})
				}
			})
		}
		if fos, farch := fileOSArch(f.name); fos != "" || farch != "" {
			if fos == "" {
				fos = runtime.GOOS
			}
			if farch == "" {
				farch = runtime.GOARCH
			}
			probes = append(probes, buildConfig{goos: fos, goarch: farch})
		}
		for _, p := range probes {
			claims = append(claims, claim{label, impPath, f.name, p, false})
		}
	}

	listings := map[string]*goListResult{}
	for _, c := range claims {
		if _, done := listings[c.cfg.key()]; done {
			continue
		}
		listings[c.cfg.key()] = runGoList(t, root, c.cfg)
	}

	for _, c := range claims {
		label := fmt.Sprintf("%s under GOOS=%s GOARCH=%s tags=%v cgo=%v",
			c.label, c.cfg.goos, c.cfg.goarch, c.cfg.tags, c.cfg.cgo)
		res := listings[c.cfg.key()]
		if res.err != nil {
			if c.wantBuilt {
				t.Errorf("%s: solver picked a configuration the go command refuses: %v", label, res.err)
			}
			// A probe the go command cannot run verifies nothing; skip it.
			continue
		}
		p := res.byImport[c.impPath]
		built := p != nil && (slices.Contains(p.GoFiles, c.file) ||
			slices.Contains(p.TestGoFiles, c.file) ||
			slices.Contains(p.XTestGoFiles, c.file))
		ignored := p != nil && slices.Contains(p.IgnoredGoFiles, c.file)
		if c.wantBuilt {
			if !built || ignored {
				t.Errorf("%s: solver picked the configuration, go list verdict built=%v ignored=%v", label, built, ignored)
			}
		} else if built || !ignored {
			t.Errorf("%s: solver reports no satisfiable configuration, go list verdict built=%v ignored=%v", label, built, ignored)
		}
	}
}

// fixtureLabel names a fixture for failure messages: its file name and
// the constraint line it carries, if any.
func fixtureLabel(name, src string) string {
	for _, line := range strings.Split(src, "\n") {
		if expr, ok := strings.CutPrefix(line, "//go:build "); ok {
			return name + " [" + strings.TrimSpace(expr) + "]"
		}
	}
	return name + " [no constraint]"
}

// goListPkg is the slice of go list -json's package the differential
// reads: the file lists the constraint decision shows up in.
type goListPkg struct {
	ImportPath     string
	GoFiles        []string
	TestGoFiles    []string
	XTestGoFiles   []string
	IgnoredGoFiles []string
}

// goListResult is one go list run keyed by import path, or the startup
// error that stopped it.
type goListResult struct {
	byImport map[string]*goListPkg
	err      error
}

// runGoList runs go list -json -e over the fixture module under cfg.
func runGoList(t *testing.T, dir string, cfg buildConfig) *goListResult {
	t.Helper()
	args := append([]string{"list", "-json", "-e"}, cfg.buildFlags()...)
	args = append(args, "./...")
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), cfg.env()...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return &goListResult{err: fmt.Errorf("go list: %v: %s", err, strings.TrimSpace(stderr.String()))}
	}
	res := &goListResult{byImport: map[string]*goListPkg{}}
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var p goListPkg
		if err := dec.Decode(&p); err == io.EOF {
			break
		} else if err != nil {
			return &goListResult{err: fmt.Errorf("decode go list output: %v", err)}
		}
		res.byImport[p.ImportPath] = &p
	}
	return res
}
