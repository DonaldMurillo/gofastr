//go:build !unix

package semantic

// openNonblock is unset off Unix: a FIFO there is not a file a watched
// directory can hold, so no open of one waits for a writer (see
// readCapped).
const openNonblock = 0
