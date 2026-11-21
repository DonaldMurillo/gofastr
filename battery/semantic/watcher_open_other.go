//go:build !unix

package semantic

import "os"

const openNonblock = 0

func openDirRoot(dir string) (*os.Root, error) { return os.OpenRoot(dir) }
