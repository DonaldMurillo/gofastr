package atomicfile

// SyncDir does nothing on Windows: NTFS journals the rename itself, and a
// directory handle cannot be flushed through os.File.Sync there.
func SyncDir(string) error { return nil }
