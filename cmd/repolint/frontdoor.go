package main

import (
	"os"
	"path/filepath"
	"strings"
)

const frameworkModule = "github.com/DonaldMurillo/gofastr"

const siteURL = "https://donaldmurillo.github.io/gofastr"

func lintFrontDoor(root string) ([]finding, error) {
	isThisRepo, err := moduleIs(root, frameworkModule)
	if err != nil || !isThisRepo {
		return nil, err
	}
	readme, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		if os.IsNotExist(err) {
			return []finding{{
				File:    "README.md",
				Line:    1,
				Rule:    "front-door-missing",
				Message: "README.md does not exist: it is the only page a stranger lands on",
			}}, nil
		}
		return nil, err
	}
	if strings.Contains(string(readme), siteURL) {
		return nil, nil
	}
	return []finding{{
		File:    "README.md",
		Line:    1,
		Rule:    "front-door-missing",
		Message: "README must link the deployed docs site (" + siteURL + "): a site nothing links to is unreachable",
	}}, nil
}

// moduleIs reports whether root's go.mod declares module; a missing
// go.mod is "no", not an error.
func moduleIs(root, module string) (bool, error) {
	body, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return moduleLine(body) == module, nil
}

// moduleLine returns the module path a go.mod declares, unquoted, or ""
// when it declares none.
func moduleLine(goMod []byte) string {
	for line := range strings.SplitSeq(string(goMod), "\n") {
		trimmed := strings.TrimSpace(line)
		rest, ok := strings.CutPrefix(trimmed, "module")
		if !ok || rest == "" || (rest[0] != ' ' && rest[0] != '\t') {
			continue
		}
		if i := strings.Index(rest, "//"); i >= 0 {
			rest = rest[:i]
		}
		return strings.Trim(strings.TrimSpace(rest), `"`)
	}
	return ""
}
