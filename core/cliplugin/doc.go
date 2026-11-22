// Package cliplugin defines the versioned extension seam for the GoFastr CLI.
//
// External modules (such as gofastr-native) and third-party Go tools import this
// package to expose namespaced subcommands (`gofastr <plugin> ...`) and, when
// explicitly enabled by the operator, pre/post command hooks and built-in
// command overrides.
//
// Security posture is fail-closed by default:
//   - Built-in command names and aliases cannot be shadowed by plugin names.
//   - Project-declared commands in gofastr.settings.yml never execute silently;
//     they require an explicit operator trust record pinned to the canonical
//     project path, command vector, approved capabilities, and binary digest.
//   - Child processes receive an allowlisted environment; GoFastr and common
//     ambient secrets are never forwarded implicitly.
//   - Hooks and built-in overrides declared in a plugin manifest stay inactive
//     until the operator opts into each one explicitly.
package cliplugin
