package main

import (
	"regexp"
	"strings"
)

var rawRPCAttribute = regexp.MustCompile("\\bdata-cui-rpc(?:[\"'`]|\\s*=)")

// isUntypedFUIWiringScope reports whether rel is a non-test Go file under
// the app generator or a battery.
func isUntypedFUIWiringScope(rel string) bool {
	if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
		return false
	}
	return strings.HasPrefix(rel, "cmd/gofastr/") || strings.HasPrefix(rel, "battery/")
}
