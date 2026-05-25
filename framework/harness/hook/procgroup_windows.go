//go:build windows

package hook

import "os/exec"

func setProcGroup(_ *exec.Cmd) {}
