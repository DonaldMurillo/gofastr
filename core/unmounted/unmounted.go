// Package unmounted is the running app's record of the framework packages
// compiled into it as unmounted copies.
package unmounted

import (
	"cmp"
	"slices"
	"strings"
	"sync"
)

// Copy is one unmounted package compiled into the binary.
type Copy struct {
	// Source is the upstream import path the copy was made from.
	Source string `json:"source"`
	// Version is the gofastr version copied: a release tag, or dev.
	Version string `json:"version"`
	// Path is the copy's own import path in the app.
	Path string `json:"path"`
}

var (
	mu     sync.Mutex
	copies = map[string]Copy{}
)

// Register records a copy under its Path, replacing an earlier one there.
func Register(c Copy) {
	mu.Lock()
	defer mu.Unlock()
	copies[c.Path] = c
}

// List returns every registered copy, sorted by Source, then Path.
func List() []Copy {
	mu.Lock()
	defer mu.Unlock()
	out := make([]Copy, 0, len(copies))
	for _, c := range copies {
		out = append(out, c)
	}
	slices.SortFunc(out, func(a, b Copy) int {
		return cmp.Or(strings.Compare(a.Source, b.Source), strings.Compare(a.Path, b.Path))
	})
	return out
}
