package cli

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

// OpenBrowser tries to open the OS default browser at url. Best-effort:
// a failure logs a hint and returns; the URL is already printed so the user
// can open it manually.
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
	if c == nil {
		return
	}
	if err := c.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "  (could not auto-open browser: %v)\n", err)
	}
}
