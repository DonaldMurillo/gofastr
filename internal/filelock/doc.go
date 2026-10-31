// Package filelock takes an exclusive OS lock on an open file, blocking
// ([Lock]) or not ([TryLock]): flock on Unix, LockFileEx on Windows.
// Closing the file releases it, and so does the process exiting, so a
// crash leaves nothing to clean up. On other systems both succeed at once.
package filelock
