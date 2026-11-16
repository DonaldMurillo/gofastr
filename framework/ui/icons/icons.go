// Package icons exposes vendored Feather and Lucide outline icons, named by
// string constants for ui.Icon. Licences: LICENSE.feather and LICENSE.lucide.
package icons

import "maps"

//go:generate go run ./internal/generate

// Names returns a caller-owned, sorted copy of the canonical names, without aliases.
func Names() []string {
	return append([]string(nil), names...)
}

// Aliases returns a caller-owned map from alternate names to canonical names.
func Aliases() map[string]string { return maps.Clone(aliases) }

// Lookup returns the trusted inner SVG markup of a built-in icon or alias,
// drawn for a 24 by 24 two-unit rounded stroke; unknown names return false.
func Lookup(name string) (string, bool) {
	if canonical, ok := aliases[name]; ok {
		name = canonical
	}
	body, ok := bodies[name]
	return body, ok
}
