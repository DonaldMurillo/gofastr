// Package sec pins the completeness side of the nondeterminism gate: a
// range over map-backed data that writes ordered output must be flagged no
// matter how the sink is spelled (an aliased fmt import is still the real
// fmt) or how the range source is expressed (maps.Keys / maps.Values
// without slices.Sorted still iterate in map order — the analyzer's own
// prescribed remediation applied with its load-bearing half dropped).
package sec

import (
	f "fmt"
	"maps"
	"strconv"
	"strings"
)

func aliasedFprintf(m map[string]string) string {
	var sb strings.Builder
	for k := range m {
		f.Fprintf(&sb, "%s", k) // want `writing to output while ranging a map`
	}
	return sb.String()
}

func aliasedFprintln(m map[string]string) string {
	var sb strings.Builder
	for k := range m {
		f.Fprintln(&sb, k) // want `writing to output while ranging a map`
	}
	return sb.String()
}

func rangeKeysStillMapOrdered(m map[string]string) string {
	var sb strings.Builder
	for k := range maps.Keys(m) {
		sb.WriteString(k) // want `writing to output while ranging a map`
	}
	return sb.String()
}

func rangeValuesStillMapOrdered(m map[string]int) string {
	var sb strings.Builder
	for v := range maps.Values(m) {
		sb.WriteString(strconv.Itoa(v)) // want `writing to output while ranging a map`
	}
	return sb.String()
}
