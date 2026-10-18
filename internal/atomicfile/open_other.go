//go:build !windows

package atomicfile

import "os"

// Open opens a file for reading while allowing its directory entry to be replaced.
func Open(path string) (*os.File, error) { return os.Open(path) }
