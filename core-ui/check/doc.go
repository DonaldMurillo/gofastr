// Package check provides an AST-based linter for .ui.go files.
//
// The linter enforces a restricted subset of Go that is safe for
// client-side hydration. It forbids goroutines, channels, I/O,
// networking, reflection, and other patterns that are not suitable
// for the declarative UI layer.
//
// Allowed imports are limited to a safe subset of the standard library
// (fmt, strings, strconv, html/template, html, math, time, errors)
// and any package under github.com/gofastr/gofastr/.
package check
