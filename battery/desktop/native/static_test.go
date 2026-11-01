package native

import (
	"context"
	"debug/elf"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// hasInterp builds ./testdata/staticmain for linux with CGO_ENABLED=0
// and the given tags, and reports whether the binary names a program
// interpreter (the glibc loader): a dynamic binary that will not start
// on Alpine or scratch.
func hasInterp(t *testing.T, tags string) bool {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "staticmain")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "build", "-tags", tags, "-o", bin, "./testdata/staticmain")
	cmd.Env = append(cmd.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH="+runtime.GOARCH)
	cmd.WaitDelay = 10 * time.Second
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build -tags %q: %v\n%s", tags, err, out)
	}
	f, err := elf.Open(bin)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, p := range f.Progs {
		if p.Type == elf.PT_INTERP {
			return true
		}
	}
	return false
}

// The builds compile the platform shells and their internal packages.
//
//gofastr:test-reads battery/desktop/

// #635: linking battery/desktop made a CGO_ENABLED=0 Linux binary
// dynamic. The gofastr_nodesktop tag keeps it static. The untagged
// build is checked too, so the test cannot pass by failing to see an
// interpreter at all.
func TestNoDesktopTagKeepsLinuxBuildStatic(t *testing.T) {
	if testing.Short() {
		t.Skip("builds two binaries")
	}
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		t.Skip("the Linux shell exists on amd64 and arm64 only")
	}
	if !hasInterp(t, "") {
		t.Fatal("the untagged build has no interpreter; the check would pass vacuously")
	}
	if hasInterp(t, "gofastr_nodesktop") {
		t.Fatal("a gofastr_nodesktop build is dynamically linked; it must be static")
	}
}
