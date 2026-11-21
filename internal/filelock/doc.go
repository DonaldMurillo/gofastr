// Package filelock takes an exclusive OS lock on an open file, blocking
// ([Lock]) or not ([TryLock]): flock on Darwin, Linux, FreeBSD, NetBSD,
// OpenBSD and DragonFly, LockFileEx on Windows. Closing the file
// releases it, and so does the process exiting, so a crash leaves
// nothing to clean up. On every other system both succeed at once,
// Unix family included: a Unix target outside the flock list above
// gets no lock at all.
package filelock
