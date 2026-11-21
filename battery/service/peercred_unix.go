//go:build darwin || linux

package service

import (
	"fmt"
	"net"
	"os"
)

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
	return checkPeerUID(uid, os.Geteuid())
}

func checkPeerUID(peer uint32, self int) error {
	if self < 0 || uint64(peer) != uint64(self) {
		return fmt.Errorf("peer uid %d is not the daemon's uid %d", peer, self)
	}
	return nil
}
