package service

import "fmt"

func checkPeerSID(peer, self string, pid uint32) error {
	if self == "" || peer != self {
		return fmt.Errorf("peer process %d runs as another user", pid)
	}
	return nil
}
