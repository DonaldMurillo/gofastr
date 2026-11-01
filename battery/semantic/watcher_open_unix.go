//go:build unix

package semantic

import (
	"os"
	"syscall"
)

// openNonblock keeps an open of a FIFO from waiting for a writer (see
// readCapped). Regular files ignore it.
const openNonblock = syscall.O_NONBLOCK

// openDirRoot opens dir as an os.Root, refusing anything that is not a
// directory during the open itself. os.OpenRoot opens without
// O_DIRECTORY and checks the type after, so a root swapped for a FIFO
// after the walk's Lstat would block the open until a writer appeared.
// Resolving "dir/." fails with ENOTDIR unless dir is a directory.
func openDirRoot(dir string) (*os.Root, error) {
	return os.OpenRoot(dir + string(os.PathSeparator) + ".")
}
