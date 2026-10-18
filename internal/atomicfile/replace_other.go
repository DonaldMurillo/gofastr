//go:build !windows

package atomicfile

import "os"

// Replace renames source over target without removing target first.
func Replace(source, target string) error { return os.Rename(source, target) }
