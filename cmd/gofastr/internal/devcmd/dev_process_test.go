package devcmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// go build prints nothing however long it runs; the heartbeat says it still is.
func TestBuildHeartbeatSaysStillBuilding(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sleep")
	}
	saved := heartbeatEvery
	heartbeatEvery = 20 * time.Millisecond
	t.Cleanup(func() { heartbeatEvery = saved })
	var err error
	out := clitest.CovT_capStdout(t, func() {
		err = RunBuildWithHeartbeat(exec.Command("sleep", "0.2"), "./cmd/app", nil)
	})
	if err != nil || !strings.Contains(out, "Building ./cmd/app") {
		t.Errorf("err %v, output %q: want a heartbeat naming the build", err, out)
	}
}

// The binary's directory is os.MkdirTemp's: owner-only, with a random suffix.
func TestDevServerBinDirIsPrivate(t *testing.T) {
	dir := DevServerBinDir()
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o700 {
		t.Errorf("%s has mode %v, want 0700", dir, info.Mode().Perm())
	}
	suffix, ok := strings.CutPrefix(filepath.Base(dir), "gofastr-dev-server-")
	if _, err := strconv.ParseUint(suffix, 10, 64); !ok || err != nil || filepath.Dir(dir) != filepath.Clean(os.TempDir()) {
		t.Errorf("%s is not a random os.MkdirTemp name under the temp root", dir)
	}
}

func TestIsolatedBinaryPathsDiffer(t *testing.T) {
	for _, key := range []string{"GOFASTR_ISOLATION", "GOFASTR_ISOLATION_APPLIED", "GOFASTR_ISOLATION_ID"} {
		t.Setenv(key, "")
	}
	dir := t.TempDir()
	writeDevFile(t, filepath.Join(dir, ".git"), "gitdir: "+filepath.Join(t.TempDir(), ".git", "worktrees", "feature")+"\n")
	writeDevFile(t, filepath.Join(dir, "gofastr.settings.yml"), "isolation:\n  enabled: true\n")
	rt, _, err := resolveDevIsolation(dir, "localhost:8080")
	if err != nil || !rt.Active() {
		t.Fatalf("setup broken: isolation %v, %v", rt.Active(), err)
	}
	plain, isolated := DevServerBinaryPath(nil), DevServerBinaryPath(rt)
	if filepath.Dir(plain) != DevServerBinDir() || filepath.Dir(isolated) != DevServerBinDir() || plain == isolated || !strings.Contains(isolated, rt.ID()) {
		t.Errorf("binary paths %q and %q: want both in %q, the isolated one named by %q", plain, isolated, DevServerBinDir(), rt.ID())
	}
}
