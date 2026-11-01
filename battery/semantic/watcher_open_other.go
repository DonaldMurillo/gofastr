//go:build !unix

package semantic

import "os"

// openNonblock is unset off Unix: a FIFO there is not a file a watched
// directory can hold, so no open of one waits for a writer (see
// readCapped).
const openNonblock = 0

// openDirRoot is os.OpenRoot off Unix, where no FIFO can stand in for
// the directory (see the Unix twin).
func openDirRoot(dir string) (*os.Root, error) { return os.OpenRoot(dir) }
