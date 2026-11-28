// Package packages embeds the canonical chrome packages in the gofastr binary.
package packages

import "embed"

// FS holds one top-level directory per canonical package.
//
//go:embed [^._]*
var FS embed.FS
