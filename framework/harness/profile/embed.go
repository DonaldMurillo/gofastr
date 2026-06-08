package profile

import (
	_ "embed"
	"fmt"
	"strings"
)

// Built-in profiles embedded into the binary so the harness works when
// run outside the source tree (an installed `gofastr` binary has no
// repo-relative profile files on disk).

//go:embed default.toml
var defaultTOML string

//go:embed framework.toml
var frameworkTOML string

// Embedded returns a built-in profile by name ("default" or
// "framework"). It is the fallback used when no on-disk profile is
// found. Unknown names return an error.
func Embedded(name string) (*Profile, error) {
	var src string
	switch name {
	case "default":
		src = defaultTOML
	case "framework":
		src = frameworkTOML
	default:
		return nil, fmt.Errorf("profile: no embedded profile %q", name)
	}
	return Parse(strings.NewReader(src))
}
