package generatecmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// --help on a verb that takes a record key or a path value prints that
// verb's flag reference and exits 0, the key or no key: the key was popped
// before the flags were parsed, so `posts patch --help` printed one usage
// line and exited 2, and the field flags it takes were nowhere to be seen.
func TestCLIKeyedVerbHelpExitsZero(t *testing.T) {
	dir := endpointsApp(t)
	bin := clitest.TestExecutablePath(filepath.Join(dir, "shop"))
	build := exec.Command("go", "build", "-o", bin, "./cmd/shop")
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, out)
	}
	home := t.TempDir()
	env := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "XDG_CONFIG_HOME=" + filepath.Join(home, ".config")}
	for _, c := range []struct {
		args []string
		want string // a flag only that verb's reference names
	}{
		{[]string{"posts", "get", "--help"}, "-url"},
		{[]string{"posts", "delete", "-h"}, "-url"},
		{[]string{"posts", "update", "--help"}, "-views int"},
		{[]string{"posts", "patch", "--help"}, "-published"},
		{[]string{"posts", "patch", "--url", "http://127.0.0.1:9", "-help"}, "-views int"},
		{[]string{"posts", "feature-post", "--help"}, "-rank int"},
	} {
		cmd := exec.Command(bin, c.args...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Errorf("shop %s: %v, want exit 0\n%s", strings.Join(c.args, " "), err, out)
			continue
		}
		if !strings.Contains(string(out), c.want) {
			t.Errorf("shop %s printed no flag reference (want %q):\n%s", strings.Join(c.args, " "), c.want, out)
		}
	}
	// A missing key with no --help is still a usage error.
	cmd := exec.Command(bin, "posts", "patch", "--views", "3")
	cmd.Env = env
	if err := cmd.Run(); err == nil {
		t.Error("patch without its id exited 0, want 2")
	} else if exitErr, ok := err.(*exec.ExitError); !ok || exitErr.ExitCode() != 2 {
		t.Errorf("patch without its id: %v, want exit 2", err)
	}
}
