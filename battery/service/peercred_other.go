//go:build !darwin && !linux && !windows

package service

import (
	"errors"
	"net"
)

// peerIsSelf refuses every connection where the package has no peer
// credential check: an unauthenticated control channel would hand
// sign-in tokens to any local user.
func peerIsSelf(*net.UnixConn) error {
	return errors.New("peer credentials are not implemented on this OS")
}
