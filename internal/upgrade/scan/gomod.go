package scan

import (
	"os"
	"path"
	"path/filepath"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"
)

// gomodMatchers reads the app's go.mod go directive and reports notes
// whose go_below it undercuts. The toolchain line never rescues it.
func (e *engine) gomodMatchers(moduleRoot string) {
	if len(e.gomodNotes) == 0 {
		return
	}
	r, err := os.OpenRoot(moduleRoot)
	if err != nil {
		return
	}
	defer r.Close()
	data, err := r.ReadFile("go.mod")
	if err != nil {
		return
	}
	f, err := modfile.Parse("go.mod", data, nil)
	if err != nil || f.Go == nil || f.Go.Syntax == nil {
		return
	}
	rel := "go.mod"
	if rp, err := filepath.Rel(e.root, moduleRoot); err == nil && rp != "." {
		rel = path.Join(filepath.ToSlash(rp), "go.mod")
	}
	for _, n := range e.gomodNotes {
		below := n.Find.GoMod.GoBelow
		if !goVersionLess(f.Go.Version, below) {
			continue
		}
		e.add(n, Hit{File: rel, Line: f.Go.Syntax.Start.Line, Why: "gomod go " + f.Go.Version + " < " + below})
	}
}

// goVersionLess compares go directive spellings as Go versions
// ("1.26.3" < "1.27"): both normalised to vMAJOR.MINOR.PATCH for semver.
// Unparsable versions compare as equal, never below.
func goVersionLess(a, b string) bool {
	va, aok := normalizeGoVersion(a)
	vb, bok := normalizeGoVersion(b)
	if !aok || !bok {
		return false
	}
	return semver.Compare(va, vb) < 0
}

func normalizeGoVersion(s string) (string, bool) {
	s = strings.TrimPrefix(strings.TrimPrefix(s, "go"), "v")
	parts := strings.Split(s, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return "", false
	}
	for len(parts) < 3 {
		parts = append(parts, "0")
	}
	for _, p := range parts {
		if p == "" {
			return "", false
		}
		for i := range len(p) {
			if p[i] < '0' || p[i] > '9' {
				return "", false
			}
		}
	}
	return "v" + strings.Join(parts, "."), true
}
