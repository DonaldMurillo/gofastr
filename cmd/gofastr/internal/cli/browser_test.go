package cli_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

func TestOpenBrowserWithoutAnOpenerPrintsAHint(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	var out string
	errOut := clitest.CovT_capStderr(t, func() {
		out = clitest.CovT_capStdout(t, func() { cli.OpenBrowser("http://127.0.0.1:1/") })
	})
	if out != "" || !strings.Contains(errOut, "could not auto-open browser") {
		t.Fatalf("stdout %q, stderr %q: want the hint on stderr only", out, errOut)
	}
}

func TestOpenBrowserDoesNotWaitForTheOpener(t *testing.T) {
	opener := map[string]string{"linux": "xdg-open", "darwin": "open"}[runtime.GOOS]
	if opener == "" {
		t.Skip("no shell opener to fake on " + runtime.GOOS)
	}
	dir := t.TempDir()
	started := filepath.Join(dir, "started")
	script := "#!/bin/sh\nPATH=/usr/bin:/bin\n: > '" + started + "'\nsleep 4\n"
	if err := os.WriteFile(filepath.Join(dir, opener), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	begin := time.Now()
	errOut := clitest.CovT_capStderr(t, func() { cli.OpenBrowser("http://127.0.0.1:1/") })
	if took := time.Since(begin); took > 2*time.Second {
		t.Fatalf("OpenBrowser took %v: it waited for an opener that runs 4s", took)
	}
	if errOut != "" {
		t.Fatalf("a started opener printed %q on stderr", errOut)
	}
	for deadline := time.Now().Add(3 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if _, err := os.Stat(started); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("OpenBrowser never ran the opener")
		}
	}
}
