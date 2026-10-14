//go:build darwin

package service

import (
	"fmt"
	"net"
	"os"

	"golang.org/x/sys/unix"
)

// peerIsSelf checks the connecting process's effective UID through
// LOCAL_PEERCRED, which the kernel fills in at connect time.
func peerIsSelf(c *net.UnixConn) error {
	raw, err := c.SyscallConn()
	if err != nil {
		return err
	}
	var cred *unix.Xucred
	var credErr error
	if err := raw.Control(func(fd uintptr) {
		cred, credErr = unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
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
