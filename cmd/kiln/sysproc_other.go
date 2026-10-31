//go:build !unix

package main

import (
	"errors"
	"os"
	"syscall"
)

func childProcessGroup() *syscall.SysProcAttr { return nil }

// openNoFollowDir opens path, refusing a symlink. There is no O_NOFOLLOW
// here; claimAdapterDir's SameFile check after the open catches a swap.
func openNoFollowDir(path string) (*os.File, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("is a symlink")
	}
	return os.Open(path)
}

// killGroup kills p only: there is no process group to signal here.
func killGroup(p *os.Process) error { return p.Kill() }
