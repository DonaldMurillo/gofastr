package service

import "fmt"

// checkPeerSID refuses a Windows peer whose token user is not the
// daemon's own. Both arguments are canonical string SIDs
// (ConvertSidToStringSid output), so string equality is SID equality;
// it lives outside peercred_windows.go so the comparison is tested on
// every OS, not only where the syscalls that read the SIDs run.
func checkPeerSID(peer, self string, pid uint32) error {
	if self == "" || peer != self {
		return fmt.Errorf("peer process %d runs as another user", pid)
	}
	return nil
}
