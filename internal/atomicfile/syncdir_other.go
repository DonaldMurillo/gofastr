//go:build !windows

package atomicfile

import "os"

// SyncDir flushes dir's entries to disk, so a file renamed into it keeps
// its new name through a power loss. Rename is atomic but not durable
// until the directory is synced: a caller that deletes the old data once
// the new file is in place (a WAL truncated after its snapshot) syncs the
// directory first.
func SyncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	if err := d.Sync(); err != nil {
		_ = d.Close()
		return err
	}
	return d.Close()
}
