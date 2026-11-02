// Package scan runs the migration registry's typed matchers against an
// app: Go through the type checker, strings as constant values with
// context, CSS through the CSS tokenizer, gofastr.yml through the YAML
// parser, go.mod through its go directive, and a per-line regex only as
// the declared last resort. It links go/packages, so nothing on the
// runtime path imports it.
package scan

import (
	"go/types"

	"github.com/DonaldMurillo/gofastr/internal/upgrade"
)

// ShapeString is the string a shapes regex is matched against for an
// object of type t: packages by name, aliases resolved. The registry
// gate in evals/upgrade-fixtures uses it to check each shapes entry
// against the symbol's real type at the release before and at the
// release itself.
func ShapeString(t types.Type) string { return shapeString(t) }

// Hit is one place a note's Find matched.
type Hit struct {
	File string // root-relative, slash-separated
	Line int    // 1-based
	Col  int    // 1-based; 0 when the matcher has no column (gomod, config)
	Why  string // what matched: a symbol, "import <path>", "class ui-button", ...
	// Err is the compile error the hit was read from when the package did
	// not type-check; "" for a typed match.
	Err string
}

// Result is one scan of an app.
type Result struct {
	// Hits holds each note's matches, sorted by File, Line, Col, deduped.
	Hits map[*upgrade.Note][]Hit
	// TypeChecked is true when every scanned package type-checked
	// against the gofastr version the app builds with today.
	TypeChecked bool
	// Broken lists the import paths of packages that did not type-check,
	// sorted.
	Broken []string
	// Unexplained holds compile errors no note matched; Why is the error.
	Unexplained []Hit
	// Unscanned lists files the load could not reach, root-relative
	// and sorted, each with its reason: "x.go (no satisfiable build
	// configuration)" for a constraint no GOOS/GOARCH/tag combination
	// satisfies, "x.go (not compiled under GOOS=… GOARCH=…)" when the
	// configuration's load did not build it. Files a load reached are
	// never listed, including those of packages that failed to
	// type-check: those carry hits through the compile-error fallback.
	Unscanned []string
}
