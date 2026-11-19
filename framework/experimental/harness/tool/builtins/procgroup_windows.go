//go:build windows

package builtins

import "os/exec"

func killGroupOnCancel(_ *exec.Cmd) {}
