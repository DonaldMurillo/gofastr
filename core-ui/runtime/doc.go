// Package runtime provides the GoFastr client-side JavaScript runtime
// as an embedded resource. The runtime handles event delegation,
// component hydration on first interaction, and SSE listeners
// for server-driven islands.
//
// Use RuntimeJS() to get the JavaScript source as a string, or
// RuntimeSize() to check the size of the runtime.
package runtime
