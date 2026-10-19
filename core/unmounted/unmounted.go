// Package unmounted is the running app's record of the framework packages
// it carries as unmounted copies. `gofastr unmount` copies a package into
// the app's tree and writes an unmounted.gen.go beside it whose init()
// calls Register, so the record holds exactly the copies compiled into the
// binary. framework.App.Unmounted and the app_unmounted MCP tool read it.
//
// The package imports nothing from gofastr: every copy imports it, and a
// copy must never pull in the package it replaced.
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
	// Version is the gofastr version copied: a release tag, or dev for a
	// copy made from a checkout.
	Version string `json:"version"`
	// Path is the copy's own import path in the app.
	Path string `json:"path"`
}

var (
	mu     sync.Mutex
	copies = map[string]Copy{}
)

// Register records a copy. It is called from the init() of the copy's
// generated unmounted.gen.go, keyed by the copy's import path: one source
// can have several copies (a chrome package copied once per binary), and
// a registration under the same path replaces the earlier one.
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
