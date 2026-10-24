package app

import (
	_ "embed"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
)

//go:embed docdir.js
var docDirJS string

// DocDirBehaviorName is the runtime module that keeps <html dir> on the
// page language's direction after a client navigation (the server's doc
// markers carry it as data-cui-dir). It loads on the first
// right-to-left marker, so a left-to-right site never fetches it and
// the core runtime carries no bytes for it.
const DocDirBehaviorName = "doc-dir"

var _ = registry.RegisterBehavior(DocDirBehaviorName, docDirJS,
	registry.Markers(`[data-cui-dir="rtl"]`))
