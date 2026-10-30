// Package filelock takes an exclusive, non-blocking OS lock on an open
// file: flock on Unix, LockFileEx on Windows. Closing the file releases
// it, and so does the process exiting, so a crash leaves nothing to
// clean up. On other systems TryLock always succeeds.
package filelock
