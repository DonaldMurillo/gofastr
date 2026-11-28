package cli

import (
	"os"
	"os/exec"
	"runtime"
)

// Version info, set by main before it dispatches.
var (
	Version   = "dev"
	Commit    = "none"
	BuildDate = "unknown"
)

// OsExit is os.Exit; tests swap it to observe an exit code.
var OsExit = os.Exit

// OpenBrowser starts the OS default browser at url and does not wait for it.
func OpenBrowser(url string) {
	var c *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		c = exec.Command("open", url)
	case "windows":
		c = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		c = exec.Command("xdg-open", url)
	}
	if err := c.Start(); err != nil {
		Fprintf(os.Stderr, "  (could not auto-open browser: %v)\n", err)
	}
}

// Min returns the smallest of vals, which must not be empty.
func Min(vals ...int) int {
	m := vals[0]
	for _, v := range vals[1:] {
		if v < m {
			m = v
		}
	}
	return m
}
