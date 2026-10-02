// Package upgrade holds the migration registry `gofastr upgrade` reads:
// one entry per release that carries migration-relevant changes, each
// note with the guidance an app needs and a Find describing exactly
// which code the change affects. The scanner that runs a Find against
// an app lives in internal/upgrade/scan; this package has no
// dependency on go/packages, so the runtime can read the retired-name
// lists without linking a type checker.
package upgrade

import "regexp"

// ModulePath is the module the registry's "gofastr/" symbol and import
// shorthand expands to.
const ModulePath = "github.com/DonaldMurillo/gofastr"

// Registry is the parsed migration registry.
type Registry struct {
	// Through is the release the registry is complete up to: releases at
	// or below it with no entry had no migration-relevant changes.
	Through  string
	Releases []Release
	// MarkerSinks are the places a kept `ui-*` component name appears
	// as an identifier rather than a class (a data-fui-comp value, a
	// registered sheet name, a drawer name). A Strings.Classes match
	// whose value only reaches marker sinks is not a hit.
	MarkerSinks MarkerSinks
}

// Release groups the notes for one tagged release.
type Release struct {
	Version string // vX.Y.Z
	Title   string
	Notes   []*Note
}

// Note is one migration-relevant change.
type Note struct {
	Change   string // one-line summary
	Breaking bool
	Guidance string // one-line, actionable
	// Find says which code the change affects. The zero Find matches
	// nothing; a breaking note carries a Find or a Nodetect reason.
	Find Find
	// Nodetect is the reason a breaking note has no Find: no spelling
	// an app carries differs between the old and the new release.
	Nodetect string
	// Version is the release the note belongs to; File the registry
	// file that holds it (releases/v0.86.0.yml) and Line its line there,
	// for parse errors and test messages.
	Version string
	File    string
	Line    int
}

// Find is the set of matchers for one note. A note's hits are the union
// of every matcher's hits. Each matcher reads the code the way its
// language means it, never as raw lines, with Text as the one explicit
// last resort.
type Find struct {
	// Uses: any reference in the app's Go code (tests included) to one
	// of these objects, resolved by the type checker against the gofastr
	// version the app builds with today. Covers calls, selectors, method
	// values, embedded promotion, composite-literal keys and generic
	// instantiations, across lines and through aliases.
	Uses []Symbol
	// Imports: an import of one of these package paths. A path ending in
	// "/..." matches the package and every package below it.
	Imports []string
	// Fields: a composite-literal field whose value meets a condition.
	Fields []FieldMatch
	// Strings: Go constant string values (after constant folding, so a
	// concatenation split across lines is one value).
	Strings StringMatch
	// CSS: .css files, read through the CSS tokenizer.
	CSS CSSMatch
	// Config: gofastr.yml keys and values, read through the YAML parser.
	Config []ConfigMatch
	// GoMod: the app's go.mod go directive.
	GoMod *GoModMatch
	// Text: last resort, a regex per line over files matching a glob,
	// for files no other matcher understands (shell scripts, JS).
	Text []TextMatch
}

// Empty reports whether f has no matcher at all.
func (f Find) Empty() bool {
	return len(f.Uses) == 0 && len(f.Imports) == 0 && len(f.Fields) == 0 &&
		f.Strings.Empty() && f.CSS.Empty() && len(f.Config) == 0 &&
		f.GoMod == nil && len(f.Text) == 0
}

// Symbol names a Go object: a package-level Name, or a Member (method
// or field) of the named type Name. Pkg is the full import path; the
// registry spells gofastr packages with the "gofastr/" shorthand, which
// the parser expands to ModulePath.
type Symbol struct {
	Pkg    string
	Name   string
	Member string // "" for a package-level object
}

// String is the registry spelling with the full import path:
// "pkg.Name" or "pkg.Name.Member".
func (s Symbol) String() string {
	if s.Member == "" {
		return s.Pkg + "." + s.Name
	}
	return s.Pkg + "." + s.Name + "." + s.Member
}

// FieldMatch matches a composite-literal field. With Key set, the
// field's value must be a map literal holding that constant string key.
// With Value set, the field's value must be a constant string matching
// it. With neither, any explicit use of the field matches (prefer Uses
// for that).
type FieldMatch struct {
	Field Symbol // Name is the struct type, Member the field
	Key   string
	Value *regexp.Regexp
}

// StringMatch matches Go constant string values.
type StringMatch struct {
	// Classes: a class token (whitespace-delimited; "x--mod" and
	// "x__part" BEM forms of a listed name count). Values that only
	// reach marker sinks are skipped.
	Classes []string
	// Attrs: an HTML attribute name, as a map-literal key or as an
	// attribute inside markup held in the string. A name ending in "-"
	// matches every attribute with that prefix.
	Attrs []string
	// Properties: a CSS custom property name (--x), declared or read
	// through var().
	Properties []string
	// Match: the value matches this regex. Last resort for strings no
	// structured matcher describes (a URL shape, a header name).
	Match *regexp.Regexp
}

// Empty reports whether m has no matcher.
func (m StringMatch) Empty() bool {
	return len(m.Classes) == 0 && len(m.Attrs) == 0 && len(m.Properties) == 0 && m.Match == nil
}

// CSSMatch matches tokens in .css files.
type CSSMatch struct {
	Classes    []string // a .name class selector (BEM forms count)
	Properties []string // a --x custom property, declared or read via var()
}

// Empty reports whether m has no matcher.
func (m CSSMatch) Empty() bool { return len(m.Classes) == 0 && len(m.Properties) == 0 }

// ConfigMatch matches gofastr.yml. Key is a dotted path from the
// document root; "*" matches any one map key or list item. With Value
// set, the scalar at the path must match it; without, the key existing
// is a hit.
type ConfigMatch struct {
	Key   string
	Value *regexp.Regexp
}

// GoModMatch matches go.mod's go directive.
type GoModMatch struct {
	// GoBelow: a go directive older than this version ("1.27") is a hit.
	GoBelow string
}

// TextMatch is the last-resort per-line regex over files whose
// root-relative slash path matches Glob ("**" crosses directories).
// Go and CSS files are never Text targets: their matchers read them
// structurally.
type TextMatch struct {
	Glob  string
	Match *regexp.Regexp
}

// MarkerSinks lists where a kept `ui-*` component name is an
// identifier, not a class.
type MarkerSinks struct {
	Calls    []ParamSink // a call argument (0-based Arg) of Func
	Fields   []Symbol    // a struct field (Name the type, Member the field)
	AttrKeys []string    // a map-literal value under one of these keys
}

// ParamSink is one call argument position.
type ParamSink struct {
	Func Symbol // a function, or a method (Name the type, Member the method)
	Arg  int
}
