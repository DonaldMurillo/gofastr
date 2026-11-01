//go:build unix

package semantic

import "syscall"

// openNonblock keeps an open of a FIFO from waiting for a writer (see
// readCapped). Regular files ignore it.
const openNonblock = syscall.O_NONBLOCK
