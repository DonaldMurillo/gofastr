//go:build darwin

package service

import "golang.org/x/sys/unix"

// peerUID reads the peer's effective UID through LOCAL_PEERCRED.
func peerUID(fd int) (uint32, error) {
	cred, err := unix.GetsockoptXucred(fd, unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
	if err != nil {
		return 0, err
	}
	return cred.Uid, nil
}
