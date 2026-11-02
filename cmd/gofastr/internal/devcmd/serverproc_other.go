//go:build !linux

package devcmd

import "os/exec"

// TieToParent is a no-op where the kernel has no parent-death signal:
// the app outlives a gofastr dev killed with SIGKILL there.
func TieToParent(*exec.Cmd) {}
