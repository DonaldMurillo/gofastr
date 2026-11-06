package desktopcmd

import (
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/mod/semver"
)

func windowsVersionParts(version string) ([4]uint16, error) {
	var parts [4]uint16
	core := version
	if end := strings.IndexAny(version, "-+"); end >= 0 {
		if !semver.IsValid("v" + version) {
			return parts, fmt.Errorf("--version %q has an invalid semver suffix", version)
		}
		core = version[:end]
	}
	if !desktopVersionRe.MatchString(core) {
		return parts, fmt.Errorf("--version %q must match %s", version, desktopVersionRe.String())
	}
	if len(version) > 256 {
		return parts, fmt.Errorf("--version is longer than 256 characters")
	}
	components := strings.Split(core, ".")
	if len(components) > len(parts) {
		return parts, fmt.Errorf("--version has more than four components; Windows file versions support at most four")
	}
	for index, component := range components {
		end := 0
		for end < len(component) && component[end] >= '0' && component[end] <= '9' {
			end++
		}
		if end == 0 {
			continue
		}
		number, err := strconv.ParseUint(component[:end], 10, 16)
		if err != nil {
			return parts, fmt.Errorf("--version component %q does not fit in a Windows file version: %w", component, err)
		}
		parts[index] = uint16(number)
	}
	return parts, nil
}
