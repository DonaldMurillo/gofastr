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
// for a serving process. Nor may it link the migration registry itself:
// the registry embeds every release note, so a package that reaches it
// is selected by every release-note edit, and framework and uihost
// reaching it once re-ran nearly the whole suite per release PR. The
// retired set reads the generated table instead. Prove both on the real
// dependency graph.
//
// The importers: directive selects this test whenever a changed package
// imports the registry, the edit that would bring it back.
//
//gofastr:test-reads importers:github.com/DonaldMurillo/gofastr/internal/upgrade
func TestServingPackagesSkipScanEngineAndRegistry(t *testing.T) {
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
	const registry = "github.com/DonaldMurillo/gofastr/internal/upgrade"
	for line := range strings.SplitSeq(string(out), "\n") {
		if strings.HasPrefix(line, "golang.org/x/tools") {
			t.Fatalf("%s is linked into a serving package; the runtime check must not depend on the scan engine", line)
		}
		if line == registry || strings.HasPrefix(line, registry+"/") {
			t.Fatalf("%s is linked into a serving package; the retired set reads retired_table.gen.go "+
				"(go generate ./internal/retired), never the registry", line)
		}
	}
}
