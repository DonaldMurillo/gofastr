package evalrunner

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// installCLIShim writes a `gofastr` wrapper into dir that appends each
// invocation's arguments to logPath and then execs realBin. Prepending dir to
// the builder's PATH gives every candidate the SNAPSHOT's CLI (guidance tells
// agents to run `gofastr docs` / `gofastr dev`; a globally installed gofastr
// would leak a different framework version into the treatment) and records a
// precise, non-deterministic funnel signal: the harness prompt names no
// command, so any `gofastr dev` in the log was discovered from the generated
// guidance alone. Transcript grepping cannot provide this signal — an agent
// merely READING the guidance echoes the command into its transcript.
func installCLIShim(dir, realBin, logPath string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		shim := "@echo off\r\n" +
			">> \"" + logPath + "\" echo %*\r\n" +
			"\"" + realBin + "\" %*\r\n"
		return os.WriteFile(filepath.Join(dir, "gofastr.cmd"), []byte(shim), 0o755)
	}
	shim := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> \"" + logPath + "\"\n" +
		"exec \"" + realBin + "\" \"$@\"\n"
	return os.WriteFile(filepath.Join(dir, "gofastr"), []byte(shim), 0o755)
}

// cliInvocationStats reads a shim log and reports how many times the builder
// invoked the gofastr CLI and whether any invocation was the dev server.
// A missing log means zero invocations, not an error — the builder simply
// never reached for the CLI.
func cliInvocationStats(logPath string) (calls int, usedDev bool) {
	f, err := os.Open(logPath)
	if err != nil {
		return 0, false
	}
	defer f.Close()
	reader := bufio.NewReader(f)
	for {
		line, readErr := reader.ReadString('\n')
		if fields := strings.Fields(line); len(fields) > 0 {
			calls++
			if fields[0] == "dev" {
				usedDev = true
			}
		}
		if readErr == io.EOF {
			return calls, usedDev
		}
		if readErr != nil {
			return calls, usedDev
		}
	}
}
