//go:build windows

package service

import (
	"fmt"
	"net"
	"unsafe"

	"golang.org/x/sys/windows"
)

const sioAFUnixGetPeerPID = 0x40000000 | 0x18000000 | 256

func peerIsSelf(c *net.UnixConn) error {
	raw, err := c.SyscallConn()
	if err != nil {
		return err
	}
	var pid uint32
	var ioErr error
	if err := raw.Control(func(fd uintptr) {
		var n uint32
		ioErr = windows.WSAIoctl(windows.Handle(fd), sioAFUnixGetPeerPID, nil, 0,
			(*byte)(unsafe.Pointer(&pid)), uint32(unsafe.Sizeof(pid)), &n, nil, 0)
	}); err != nil {
		return err
	}
	if ioErr != nil {
		return fmt.Errorf("read peer pid: %w", ioErr)
	}
	peer, err := processUserSID(pid)
	if err != nil {
		return err
	}
	self, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return fmt.Errorf("read own token: %w", err)
	}
	return checkPeerSID(peer.String(), self.User.Sid.String(), pid)
}

func processUserSID(pid uint32) (*windows.SID, error) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return nil, fmt.Errorf("open peer process %d: %w", pid, err)
	}
	defer windows.CloseHandle(h)
	var tok windows.Token
	if err := windows.OpenProcessToken(h, windows.TOKEN_QUERY, &tok); err != nil {
		return nil, fmt.Errorf("open peer token: %w", err)
	}
	defer tok.Close()
	u, err := tok.GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("read peer token: %w", err)
	}
	return u.User.Sid.Copy()
}
