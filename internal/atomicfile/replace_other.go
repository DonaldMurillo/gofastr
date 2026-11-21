//go:build !windows

package atomicfile

import "os"

// Replace renames source over target without removing target first.
func Replace(source, target string) error { return os.Rename(source, target) }

// ReplaceAt renames source over target, both slash-separated names
// already resolved inside root, without removing target first.
func ReplaceAt(root *os.Root, source, target string) error {
	return root.Rename(source, target)
}
