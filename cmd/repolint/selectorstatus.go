package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// affectedTested matches a `[ -n "$(… cmd/affected …)" ]` (or -z, a `!`
// before the operator, or `[[`, whose inner bracket the pattern also
// matches) test.
// Inside a test the command substitution's exit status is lost, so a
// selector that fails prints nothing and reads as "nothing affected": the
// step skips a suite where it should fail. Assigning the output on its own
// line keeps the status, and the step's `bash -e` stops on it.
var affectedTested = regexp.MustCompile(`\[\s+(!\s+)?-[nz]\s+"?\$\([^)]*cmd/affected`)

const affectedTestedMessage = "tests a cmd/affected command substitution inside [ ]: the selector's exit status is lost, so a failure reads as nothing affected and the suite skips; assign the output on its own line, then test the variable"

// lintSelectorStatus applies the selector-status rule to the workflow
// files, which lintRepo's walk skips with the other dot directories.
// Build scripts get the same rule from lintBytes.
func lintSelectorStatus(root string) ([]finding, error) {
	paths, err := filepath.Glob(filepath.Join(root, ".github", "workflows", "*.y*ml"))
	if err != nil {
		return nil, err
	}
	var out []finding
	for _, p := range paths {
		body, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			rel = p
		}
		out = append(out, selectorStatusFindings(filepath.ToSlash(rel), body)...)
	}
	return out, nil
}

func selectorStatusFindings(rel string, body []byte) []finding {
	var out []finding
	for i, line := range strings.Split(string(body), "\n") {
		if affectedTested.MatchString(line) {
			out = append(out, finding{
				File:    rel,
				Line:    i + 1,
				Rule:    "selector-status-discarded",
				Message: affectedTestedMessage,
			})
		}
	}
	return out
}
