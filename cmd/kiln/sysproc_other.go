//go:build !unix

package main

import (
	"errors"
	"os"
	"syscall"
)

func childProcessGroup() *syscall.SysProcAttr { return nil }

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

func killGroup(p *os.Process) error { return p.Kill() }
