// Package compile transpiles a restricted subset of Go to JavaScript.
//
// It processes Render() and Actions() methods from .ui.go files and
// produces JavaScript that can run in the browser as part of the
// GoFastr client-side hydration layer.
//
// Supported patterns include function calls, string concatenation,
// if/else, for/range loops, variable declarations, struct literals,
// field access, method calls, and return statements.
//
// Unsupported patterns emit a /* unsupported: ... */ comment rather
// than causing an error, making this a progressive compiler.
package compile
