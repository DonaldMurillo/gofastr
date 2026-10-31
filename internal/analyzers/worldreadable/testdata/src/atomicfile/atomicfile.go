// Package atomicfile stands in for internal/atomicfile.
package atomicfile

import "os"

func Rewrite(path string, data []byte, perm os.FileMode) error { return nil }
