//go:build linux

package service

import "golang.org/x/sys/unix"

// peerUID reads the peer's UID through SO_PEERCRED.
func peerUID(fd int) (uint32, error) {
	cred, err := unix.GetsockoptUcred(fd, unix.SOL_SOCKET, unix.SO_PEERCRED)
	if err != nil {
		return 0, err
	}
	return cred.Uid, nil
}
