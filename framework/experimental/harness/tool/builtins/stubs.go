package builtins

// Write creates or overwrites a file, making its parent directories.
type Write = writeImpl

// Edit replaces an exact string in a regular file, refusing an ambiguous match unless replace_all is set.
type Edit = editImpl

// Bash runs a command through /bin/sh -c with a timeout, an output cap and a credential-tool blocklist.
type Bash = bashImpl

// WebFetch fetches an http(s) URL with GET, refusing any internal address on every hop.
type WebFetch = webFetchImpl
