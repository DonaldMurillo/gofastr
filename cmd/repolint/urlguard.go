package main

import (
	"bytes"
	"strings"
)

// duplicateURLGuardLine reports the line of a re-derived URL-scheme
// allow-list, or 0 when the file has none.
func duplicateURLGuardLine(rel string, body []byte) int {
	if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
		return 0
	}
	if strings.HasPrefix(rel, "core-ui/urlsafe/") || strings.HasPrefix(rel, "cmd/repolint/") {
		return 0
	}
	if !bytes.Contains(body, []byte(`"%0d"`)) || !bytes.Contains(body, []byte(`"%0a"`)) {
		return 0
	}
	for i, line := range strings.Split(string(body), "\n") {
		if strings.Contains(line, `"%0d"`) {
			return i + 1
		}
	}
	return 1
}
