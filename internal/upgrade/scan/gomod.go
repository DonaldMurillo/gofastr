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
// The order is the go command's own: a bare language version sits
// below every release of its minor and a prerelease below the final
// release, so 1.26 < 1.26rc1 < 1.26.0 < 1.27, and a go 1.27rc1
// directive is not below 1.27. Unparsable versions compare as equal,
// never below.
func goVersionLess(a, b string) bool {
	va, aok := normalizeGoVersion(a)
	vb, bok := normalizeGoVersion(b)
	if !aok || !bok {
		return false
	}
	return semver.Compare(va, vb) < 0
}

// goPrerelease splits a trailing Go prerelease suffix (rcN, betaN,
// alphaN) off a version: "1.26rc1" is ("1.26", "rc.1"). The dotted
// form makes semver order rc2 below rc10 and beta below rc.
func goPrerelease(s string) (string, string, bool) {
	for _, tag := range []string{"alpha", "beta", "rc"} {
		i := strings.LastIndex(s, tag)
		if i <= 0 {
			continue
		}
		n := s[i+len(tag):]
		if !allDigits(n) {
			return "", "", false
		}
		return s[:i], tag + "." + n, true
	}
	return s, "", true
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func normalizeGoVersion(s string) (string, bool) {
	s = strings.TrimPrefix(strings.TrimPrefix(s, "go"), "v")
	s, pre, ok := goPrerelease(s)
	if !ok {
		return "", false
	}
	parts := strings.Split(s, ".")
	if len(parts) < 2 || len(parts) > 3 || (pre != "" && len(parts) != 2) {
		return "", false
	}
	bare := len(parts) == 2 && pre == ""
	for len(parts) < 3 {
		parts = append(parts, "0")
	}
	for _, p := range parts {
		if !allDigits(p) {
			return "", false
		}
	}
	v := "v" + strings.Join(parts, ".")
	switch {
	case pre != "":
		v += "-" + pre
	case bare:
		// Go orders a bare language version below every release of
		// its minor, prereleases included: 1.26 < 1.26rc1 < 1.26.0.
		// A numeric prerelease sorts below an alphanumeric one, so
		// v1.26.0-0 lands under v1.26.0-rc.1 and under v1.26.0.
		v += "-0"
	}
	return v, true
}
