//go:build !darwin && !linux && !windows

package service

import (
	"errors"
	"net"
)

func peerIsSelf(*net.UnixConn) error {
	return errors.New("peer credentials are not implemented on this OS")
}
