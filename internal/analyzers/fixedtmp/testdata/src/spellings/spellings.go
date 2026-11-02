// Package spellings holds the shared-temp-root spellings the 2026-11-02
// guard bash found fixedtmp blind to: a concat whose first operand is
// "/tmp/" (the slash on the literal, not a separate "/" operand), a Join
// rooted at a literal deeper under /tmp, fmt.Sprintf with a "/tmp/..."
// format, and an exec.Cmd literal's Dir field.
package spellings

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
)

func concatSlash() error {
	return os.MkdirAll("/tmp/gofastr-"+strconv.Itoa(os.Getpid()), 0o700) // want `mkdir on a fixed path under the shared temp root`
}

func joinDeeper(name string) error {
	return os.WriteFile(filepath.Join("/tmp/gofastr", name), nil, 0o600) // want `file created at a fixed path under the shared temp root`
}

func sprintfPid() (*os.File, error) {
	return os.Create(fmt.Sprintf("/tmp/gofastr-%d.lock", os.Getpid())) // want `file created at a fixed path under the shared temp root`
}

func sprintfRoot() error {
	return os.Mkdir(fmt.Sprintf("%s/gofastr-%d", os.TempDir(), os.Getpid()), 0o700) // want `mkdir on a fixed path under the shared temp root`
}

func cmdLiteral() *exec.Cmd {
	return &exec.Cmd{Path: "/bin/sh", Dir: "/tmp/kiln-agent"} // want `exec Dir is a fixed path under the shared temp root`
}

// Silent: not under the temp root, and minted names.
func elsewhere(name string) error {
	return os.MkdirAll("/tmpfiles/"+name, 0o700)
}

func minted() error {
	dir, err := os.MkdirTemp("", "gofastr-")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "x"), nil, 0o600)
}
