//go:build unix

package semantic

import (
	"os"
	"syscall"
)

const openNonblock = syscall.O_NONBLOCK

func openDirRoot(dir string) (*os.Root, error) {
	return os.OpenRoot(dir + string(os.PathSeparator) + ".")
}
