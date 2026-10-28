// Package joined pins the one-step-removed arm (2026-10-28): a slice
// collected in map order that reaches output unsorted.
package joined

import (
	"fmt"
	"io"
	"maps"
	"slices"
	"sort"
	"strings"
)

// classes is core-ui/html Classes.
func classes(set map[string]bool) string {
	var active []string
	for cls, on := range set {
		if on {
			active = append(active, cls) // want `active collects a map range in iteration order and is joined or written unsorted`
		}
	}
	return strings.Join(active, " ")
}

// writeLoop writes the collected slice in a later loop.
func writeLoop(w io.Writer, m map[string]int) {
	var lines []string
	for k, v := range m {
		lines = append(lines, fmt.Sprintf("%s=%d", k, v)) // want `lines collects a map range`
	}
	for _, l := range lines {
		fmt.Fprintln(w, l)
	}
}

// viaKeys ranges the unsorted maps.Keys iterator.
func viaKeys(m map[string]int) string {
	var ks []string
	for k := range maps.Keys(m) {
		ks = append(ks, k) // want `ks collects a map range`
	}
	return strings.Join(ks, ",")
}

// sorted is the fix: sort before joining.
func sorted(set map[string]bool) string {
	var active []string
	for cls := range set {
		active = append(active, cls)
	}
	slices.Sort(active)
	return strings.Join(active, " ")
}

// sortPkg sorts with package sort.
func sortPkg(set map[string]bool) string {
	var active []string
	for cls := range set {
		active = append(active, cls)
	}
	sort.Strings(active)
	return strings.Join(active, " ")
}

// sortedKeys ranges a sorted slice: never a map range.
func sortedKeys(m map[string]int) string {
	var ks []string
	for _, k := range slices.Sorted(maps.Keys(m)) {
		ks = append(ks, k)
	}
	return strings.Join(ks, ",")
}

// returned leaves ordering to the caller: not joined or written here.
func returned(m map[string]int) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

// fanout calls each collected callback: order-free, no output.
func fanout(subs map[int]func()) {
	var fns []func()
	for _, fn := range subs {
		fns = append(fns, fn)
	}
	for _, fn := range fns {
		fn()
	}
}

// joinedOther joins a different slice.
func joinedOther(m map[string]int, other []string) string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	_ = ks
	return strings.Join(other, ",")
}

// joinedBefore joins before the map range runs.
func joinedBefore(m map[string]int) string {
	var ks []string
	head := strings.Join(ks, ",")
	for k := range m {
		ks = append(ks, k)
	}
	return head
}

// single iterates once under a len guard.
func single(m map[string]int) string {
	var ks []string
	if len(m) == 1 {
		for k := range m {
			ks = append(ks, k)
		}
	}
	return strings.Join(ks, ",")
}

var global []string

// pkgLevel appends to a package variable: not this function's slice.
func pkgLevel(m map[string]int) string {
	for k := range m {
		global = append(global, k)
	}
	return strings.Join(global, ",")
}
