//go:build !linux

package devcmd

import "os/exec"

// TieToParent does nothing where the kernel has no parent-death signal.
func TieToParent(*exec.Cmd) {}
