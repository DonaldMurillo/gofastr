// Package packages embeds the canonical chrome packages in the gofastr binary.
package packages

import "embed"

// FS holds one top-level directory per canonical package, beside this
// package's own top-level files, which readers skip.
//
//go:embed [^._]*
var FS embed.FS
