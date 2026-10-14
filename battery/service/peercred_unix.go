//go:build darwin || linux

package service

import (
	"fmt"
	"net"
	"os"
)

// peerIsSelf checks the connecting process's UID, which the kernel
// fills in at connect time (peerUID reads it per OS).
func peerIsSelf(c *net.UnixConn) error {
	raw, err := c.SyscallConn()
	if err != nil {
		return err
	}
	var uid uint32
	var credErr error
	if err := raw.Control(func(fd uintptr) {
		uid, credErr = peerUID(int(fd))
	}); err != nil {
		return err
	}
	if credErr != nil {
		return fmt.Errorf("read peer credentials: %w", credErr)
	}
	if int(uid) != os.Geteuid() {
		return fmt.Errorf("peer uid %d is not the daemon's uid %d", uid, os.Geteuid())
	}
	return nil
}
