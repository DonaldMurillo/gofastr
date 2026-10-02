package retired

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The runtime check must never link the scan engine: internal/upgrade/
// scan's go/packages dependency is for `gofastr upgrade`'s binary, not
// for a serving process. Prove it on the real dependency graph.
func TestServingPackagesSkipXTools(t *testing.T) {
	if testing.Short() {
		t.Skip("shells out to the go tool")
	}
	repo, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	pkgs := []string{
		"./framework/uihost", // hooks the scan
		"./framework",        // TestHarness installs the reporter
		"./internal/retired", // the scanner + set
		"./internal/upgrade", // the registry the set reads
	}
	args := append([]string{"list", "-deps"}, pkgs...)
	cmd := exec.Command("go", args...)
	cmd.Dir = repo
	// Hermetic: resolve from the local module graph only.
	cmd.Env = append(os.Environ(), "GOPROXY=off", "GOFLAGS=-mod=mod", "GOWORK=off")
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			t.Fatalf("go list -deps %s: %v: %s", strings.Join(pkgs, " "), err, ee.Stderr)
		}
		t.Fatal(err)
	}
	for line := range strings.SplitSeq(string(out), "\n") {
		if strings.HasPrefix(line, "golang.org/x/tools") {
			t.Fatalf("%s is linked into a serving package; the runtime check must not depend on the scan engine", line)
		}
	}
}
