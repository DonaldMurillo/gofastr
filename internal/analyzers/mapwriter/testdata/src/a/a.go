package a

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

func bad(m map[string]string) string {
	var sb strings.Builder
	for k, v := range m {
		sb.WriteString(k) // want `writing to output while ranging a map`
		sb.WriteString(v) // want `writing to output while ranging a map`
	}
	return sb.String()
}

func badFprintf(m map[string]int) string {
	var sb strings.Builder
	for k := range m {
		fmt.Fprintf(&sb, "%s", k) // want `writing to output while ranging a map`
	}
	return sb.String()
}

func goodSorted(m map[string]string) string {
	var sb strings.Builder
	for _, k := range slices.Sorted(maps.Keys(m)) {
		sb.WriteString(m[k])
	}
	return sb.String()
}

func goodSingleEntry(m map[string]string) string {
	var sb strings.Builder
	if len(m) == 1 {
		for _, v := range m {
			sb.WriteString(v)
		}
	}
	return sb.String()
}

func goodMutation(m map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range m {
		out[k] = v
	}
	return out
}
