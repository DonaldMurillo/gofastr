package main

import (
	"database/sql"
	"sort"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework"
)

// screenRegistrar pairs a screen's mount func with its declaration order.
// Each screen file appends one or more screenRegistrars in init();
// mountGenerated runs them in declaration order, so the package behaves
// identically regardless of the lexical order Go runs each file's init()
// in. The order field is also how gofastr pack recovers the blueprint's
// authored screen order from source.
type screenRegistrar struct {
	order int
	fn    func(fwApp *framework.App, site *app.App, db *sql.DB)
}

var screenRegistrars []screenRegistrar

// mountGenerated mounts every generated screen with site, in declaration
// order. This file never holds a screen or entity name: add a screen by
// dropping in a new screen_<name>.go that appends to screenRegistrars in
// init(). Entity resource wiring (appResources) lives in the per-entity
// screen_<entity>_crud.go files, never here.
func mountGenerated(fwApp *framework.App, site *app.App, db *sql.DB) {
	sort.SliceStable(screenRegistrars, func(i, j int) bool {
		return screenRegistrars[i].order < screenRegistrars[j].order
	})
	for _, r := range screenRegistrars {
		r.fn(fwApp, site, db)
	}
}
