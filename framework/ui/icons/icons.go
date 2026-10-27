// Package icons exposes vendored Feather and Lucide outline icons.
//
// Names are string constants for use with ui.Icon and ui.LinkButton.
// Unmodified upstream SVGs live in svg/; sources.json records the pinned commit,
// retrieval time, source URLs, and checksums. Drawings use a 24 by 24 grid and a
// two-unit rounded stroke. See LICENSE.feather and LICENSE.lucide for copyright and
// redistribution terms. The generated drawing data retains that license notice.
package icons

import "maps"

//go:generate go run ./internal/generate

// Names returns the canonical icon names in alphabetical order. The returned
// slice belongs to the caller. Compatibility aliases are not included.
func Names() []string {
	return append([]string(nil), names...)
}

// Aliases returns a caller-owned map from alternate names to canonical names.
// All aliases also have generated string constants and work with ui.Icon.
func Aliases() map[string]string { return maps.Clone(aliases) }

// Lookup returns trusted inner SVG markup for a built-in icon. The caller
// supplies the SVG wrapper with viewBox="0 0 24 24", fill="none",
// stroke="currentColor", stroke-width="2", and rounded caps and joins.
// Alternate names resolve through Aliases; each target is a canonical drawing.
// Unknown names return an empty body and false.
func Lookup(name string) (string, bool) {
	if canonical, ok := aliases[name]; ok {
		name = canonical
	}
	body, ok := bodies[name]
	return body, ok
}
