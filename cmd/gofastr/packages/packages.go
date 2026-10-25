// Package packages carries the canonical chrome packages into the
// gofastr binary. Each subdirectory (siteheader, sitefooter, docpage) is
// an ordinary package that generated apps import; this file only holds
// the embed, and it lives here because go:embed cannot reach a parent
// directory. `gofastr generate package` and the app spec's marketing
// chrome copy from FS, so the directory stays the single source of truth.
package packages

import "embed"

// FS is every canonical package, rooted at this directory: FS's
// top-level directories are the package names. This file sits at the
// root too, and readers skip anything that is not a directory there.
// The pattern skips names starting with '.' or '_', the rule a
// directory embed applies below the top level: local state such as an
// ignored .gofastr/ never ships in the binary.
//
//go:embed [^._]*
var FS embed.FS
