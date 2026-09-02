// Package afalt holds asciifold positives in names and a layout that
// never existed in this repo: the shape, not the site.
package afalt

import "strings"

type gadget struct {
	Name string
}

var gadgets = map[string]*gadget{
	"slider": {Name: "slider"},
}

// direct is the inline spelling: ToLower straight into the index of a
// pointer-valued registry.
func direct(name string) *gadget {
	return gadgets[strings.ToLower(name)] // want `registry lookup folds Unicode case`
}

// throughLocal binds the fold first.
func throughLocal(name string) bool {
	folded := strings.ToUpper(name) // the fold itself is not the finding
	g, ok := gadgets[folded]        // want `registry lookup folds Unicode case`
	return ok && g != nil
}

// constructors is a func-valued registry.
var constructors = map[string]func() *gadget{
	"slider": func() *gadget { return &gadget{Name: "slider"} },
}

func build(kind string) *gadget {
	ctor, ok := constructors[strings.ToLower(kind)] // want `registry lookup folds Unicode case`
	if !ok {
		return nil
	}
	return ctor()
}

// equalFoldGrant is the EqualFold arm: an ASCII constant guards a
// registry lookup, so a homoglyph passes the guard.
func equalFoldGrant(action string) *gadget {
	if strings.EqualFold(action, "slider") { // want `EqualFold against an ASCII constant`
		if g, ok := gadgets[action]; ok {
			return g
		}
	}
	return nil
}

// pinnedFirst refuses non-ASCII before folding: the fixed posture.
func pinnedFirst(name string) *gadget {
	if strings.ContainsFunc(name, func(r rune) bool { return r >= 0x80 }) {
		return nil
	}
	return gadgets[strings.ToLower(name)]
}

// ---- deliberate silences ------------------------------------------------

// plainValues: string-valued map, an alias table — no entry to
// impersonate.
func plainValues(alias string, m map[string]string) string {
	return m[strings.ToLower(alias)]
}

// displayFold never indexes: casing text for humans.
func displayFold(name string) string {
	return strings.ToUpper(name)
}
