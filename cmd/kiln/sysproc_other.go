//go:build !unix

package main

import (
	"os"
	"syscall"
)

func childProcessGroup() *syscall.SysProcAttr { return nil }

// killGroup kills p only: there is no process group to signal here.
func killGroup(p *os.Process) error { return p.Kill() }
