//go:build windows

package builtins

import "os/exec"

// killGroupOnCancel is a no-op on Windows: the Bash tool needs /bin/sh,
// and WaitDelay still bounds Run.
func killGroupOnCancel(_ *exec.Cmd) {}
