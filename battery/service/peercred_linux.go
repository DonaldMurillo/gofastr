//go:build linux

package service

import (
	"fmt"
	"net"
	"os"

	"golang.org/x/sys/unix"
)

// peerIsSelf checks the connecting process's UID through SO_PEERCRED,
// which the kernel fills in at connect time.
func peerIsSelf(c *net.UnixConn) error {
	raw, err := c.SyscallConn()
	if err != nil {
		return err
	}
	var cred *unix.Ucred
	var credErr error
	if err := raw.Control(func(fd uintptr) {
		cred, credErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return err
	}
	if credErr != nil {
		return fmt.Errorf("read peer credentials: %w", credErr)
	}
	if int(cred.Uid) != os.Geteuid() {
		return fmt.Errorf("peer uid %d is not the daemon's uid %d", cred.Uid, os.Geteuid())
	}
	return nil
}
