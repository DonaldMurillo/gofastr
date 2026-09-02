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

// ---- one indirection deeper -------------------------------------------

// norm is the one-line fold helper any package that normalizes more
// than once factors; upper is the func-variable spelling.
func norm(s string) string { return strings.ToLower(s) }

var upper = strings.ToUpper

func viaHelper(name string) *gadget {
	return gadgets[norm(name)] // want `registry lookup folds Unicode case`
}

func viaVar(name string) *gadget {
	return gadgets[upper(name)] // want `registry lookup folds Unicode case`
}

// ---- struct-held registries -------------------------------------------

type gadgetRegistry struct {
	byName map[string]*gadget
}

// put/get fold on both sides: the symmetric comparison the both-sides
// silence covers — field-held like local.
func (r *gadgetRegistry) put(n string, g *gadget) {
	r.byName[strings.ToLower(n)] = g
}

func (r *gadgetRegistry) get(n string) *gadget {
	return r.byName[strings.ToLower(n)]
}

// widgetIndex is populated with plain keys and read with a fold.
var widgetIndex = struct{ m map[string]*gadget }{m: map[string]*gadget{"slider": {}}}

func lookupWidget(name string) *gadget {
	return widgetIndex.m[strings.ToLower(name)] // want `registry lookup folds Unicode case`
}

// ---- EqualFold on human text -------------------------------------------

var wordcount = map[string]int{}

// humanText: the EqualFold compares what the user typed against a
// word; the earlier if merely counts words and must not leak onto it.
func humanText(name string) bool {
	if name != "" {
		wordcount[name]++
	}
	return strings.EqualFold(name, "slider")
}

// ---- membership tests --------------------------------------------------

type perm struct{ Level int }

var allowlist = map[string]perm{"deploy": {3}}
var denylist = map[string]perm{"drop": {0}}
var grants = map[string]perm{"rollback": {1}}

// allowedAllowlist: folding an allow-list lookup makes the grant
// weaker — a homoglyph inherits the entry's permission.
func allowedAllowlist(action string) bool {
	_, ok := allowlist[strings.ToLower(action)] // want `registry lookup folds Unicode case`
	return ok
}

// blockedDenylist: folding onto a deny list only makes it stricter —
// declared silence, the map name says denial.
func blockedDenylist(action string) bool {
	_, ok := denylist[strings.ToLower(action)]
	return ok
}

// allowedNegated: the ok result is used negated, the deny posture —
// declared silence.
func allowedNegated(action string) bool {
	_, ok := grants[strings.ToLower(action)]
	return !ok
}

// ---- length guards are not ASCII pins ----------------------------------

// longGuard: a length bound shares nothing with the value's bytes —
// "ſ" is two bytes long and passes — so it pins nothing.
func longGuard(name string) *gadget {
	if len(name) >= 128 {
		return nil
	}
	return gadgets[strings.ToLower(name)] // want `registry lookup folds Unicode case`
}

// bytePin: an index view against the boundary pins — the fixed
// posture, byte spelling.
func bytePin(name string, i int) *gadget {
	if name[i] >= 0x80 {
		return nil
	}
	return gadgets[strings.ToLower(name)]
}
