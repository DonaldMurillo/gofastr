// Package afcmp holds the fold-then-compare fixtures. The positives are
// reduced from framework/sdkdocs/sdkdocs.go lookupAll and
// core/upload/validate.go ValidateExt before the 2026-10-27 bug bash fix
// (W18): both operands fold, so the KELVIN SIGN spelling "Kiosk"
// equals "kiosk" and a homoglyph names a real entity or passes an
// extension allow list.
package afcmp

import "strings"

type entity struct{ Name, Table string }

// lookupAll is the pre-fix sdkdocs shape: a folded URL segment against
// folded entity names, through a local.
func lookupAll(segment string, all []entity) []entity {
	segment = strings.ToLower(segment)
	var out []entity
	for _, e := range all {
		if segment == strings.ToLower(e.Table) || segment == strings.ToLower(e.Name) { // want `folded comparison` `folded comparison`
			out = append(out, e)
		}
	}
	return out
}

// validateExt is the pre-fix upload shape: two direct folds, != too.
func validateExt(ext string, allowed []string) bool {
	for _, a := range allowed {
		if strings.ToLower(a) != strings.ToLower(ext) { // want `folded comparison`
			continue
		}
		return true
	}
	return false
}

// lowerASCII is the fix's spelling: only A-Z fold, so a non-ASCII
// look-alike keeps its bytes and never equals an ASCII name.
func lowerASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if 'A' <= c && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}

func lookupFixed(segment string, all []entity) []entity {
	segment = lowerASCII(segment)
	var out []entity
	for _, e := range all {
		if segment == lowerASCII(e.Table) || segment == lowerASCII(e.Name) {
			out = append(out, e)
		}
	}
	return out
}

// One side folded and the other a constant or an unfolded value is not
// this shape (a lowercase check, a fixed keyword): silent.
func isLower(s string) bool { return s == strings.ToLower(s) }

func isClass(k string) bool { return strings.ToLower(k) == "class" }

// A value pinned ASCII first cannot carry a look-alike: silent.
func lookupPinned(segment string, all []entity) []entity {
	for i := 0; i < len(segment); i++ {
		if segment[i] >= 0x80 {
			return nil
		}
	}
	key := strings.ToLower(segment)
	var out []entity
	for _, e := range all {
		if key == strings.ToLower(e.Name) {
			out = append(out, e)
		}
	}
	return out
}
