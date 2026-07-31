package builtins

// Type aliases for the public catalog. The Tool implementations live
// next to their behavior (write.go, bash.go, webfetch.go). Aliasing
// here keeps a single place to look up the catalog shape.

// Write is the public alias for the file-write tool implementation.
type Write = writeImpl

// Edit is the public alias for the file-edit tool implementation.
type Edit = editImpl

// Bash is the public alias for the shell-execution tool implementation.
type Bash = bashImpl

// WebFetch is the public alias for the HTTP-GET tool implementation.
type WebFetch = webFetchImpl
