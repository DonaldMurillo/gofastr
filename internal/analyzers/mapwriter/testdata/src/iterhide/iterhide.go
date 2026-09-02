// Package iterhide pins the range-source side of the gate: maps.Keys
// still walks the map when its result reaches the range statement
// through an intermediate value instead of directly — bound to a
// variable first, or collected into a slice without sorting.
package iterhide

import (
	"maps"
	"slices"
	"strconv"
	"strings"
)

func iteratorVariable(m map[int]string) string {
	var sb strings.Builder
	keys := maps.Keys(m)
	for k := range keys {
		sb.WriteString(strconv.Itoa(k)) // want `writing to output while ranging a map`
	}
	return sb.String()
}

func collectedKeys(m map[int]string) string {
	var sb strings.Builder
	for k := range slices.Collect(maps.Keys(m)) {
		sb.WriteString(strconv.Itoa(k)) // want `writing to output while ranging a map`
	}
	return sb.String()
}
