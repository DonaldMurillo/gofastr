//go:build !unix

package devloop

import "os/exec"

func detach(*exec.Cmd) {}

// The shims are sh scripts; a Windows host would need .cmd twins.
const supported = false
