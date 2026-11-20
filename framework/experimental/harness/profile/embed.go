package profile

import (
	_ "embed"
	"fmt"
	"strings"
)

//go:embed default.toml
var defaultTOML string

//go:embed framework.toml
var frameworkTOML string

// Embedded parses the built-in profile "default" or "framework"; any other name is an error.
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
