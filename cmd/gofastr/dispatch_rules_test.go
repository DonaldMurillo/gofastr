package main

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/core/cliplugin"
)

func TestHelpFlagNeverRunsACommandWithoutItsOwnHelp(t *testing.T) {
	isolatePluginEnv(t)
	ran := false
	saved := commands
	commands = append(append([]commandSpec(nil), saved...), commandSpec{names: []string{"zz-probe"}, run: func([]string) { ran = true }})
	t.Cleanup(func() { commands = saved })
	for _, args := range [][]string{
		{"zz-probe", "--help"},
		{"zz-probe", "start", "-h"},
		{"version", "--help"},
		{"zz-unknown", "x", "--help"},
	} {
		ran = false
		out, _, code := runDispatch(t, args...)
		if ran || code != 0 || !strings.Contains(out, "gofastr <command> [arguments]") || strings.Contains(out, "commit:") {
			t.Errorf("%q: ran %v, exit %d, want the overview and nothing run:\n%s", args, ran, code, out)
		}
	}
}

func TestUnknownCommandSuggestsPluginsUnlessTheyAreOff(t *testing.T) {
	isolatePluginEnv(t)
	clitest.CovT_chdir(t, t.TempDir())
	bin := buildMockPluginBinary(t, mockNativePluginSrc, t.TempDir(), "gofastr-native")
	if _, errOut, code := runDispatch(t, "plugin", "add", "native", bin); code != 0 {
		t.Fatalf("plugin add native: exit %d: %s", code, errOut)
	}
	out, _, code := runDispatch(t, "nativ")
	if code != 2 || !strings.Contains(out, "Did you mean: gofastr native?") {
		t.Fatalf("nativ: exit %d, want a suggestion of the plugin:\n%s", code, out)
	}
	t.Setenv(cliplugin.EnvBypassPlugins, "1")
	out, errOut, code := runDispatch(t, "native")
	if code != 2 || !strings.Contains(errOut, "Unknown command: native") || strings.Contains(out, "Did you mean: gofastr native?") {
		t.Fatalf("native with plugins off: exit %d, want the unknown-command answer without the plugin:\n%s%s", code, out, errOut)
	}
}

func TestJSONHelpSkipsPluginHooks(t *testing.T) {
	isolatePluginEnv(t)
	clitest.CovT_chdir(t, t.TempDir())
	bin := buildMockPluginBinary(t, `package main
import (
	"fmt"
	"os"
)
func main() {
	if len(os.Args) == 2 && os.Args[1] == "--gofastr-plugin-manifest" {
		fmt.Print(`+"`"+`{"protocol_version":1,"name":"jhook","version":"v0.1.0","summary":"JSON pre-hook","hooks":[{"phase":"pre","command":"verify"}]}`+"`"+`)
		return
	}
	os.Exit(9)
}
`, t.TempDir(), "jhook")
	if _, errOut, code := runDispatch(t, "plugin", "add", "jhook", bin, "--allow-hook", "pre:verify"); code != 0 {
		t.Fatalf("plugin add jhook: exit %d: %s", code, errOut)
	}
	out, errOut, code := runDispatch(t, "verify", "--json", "--help")
	if code != 0 || out != "" || !strings.Contains(errOut, "Usage") {
		t.Fatalf("verify --json --help: exit %d, stdout %q, stderr %q; want usage on stderr and no hook run", code, out, errOut)
	}
}

func TestBuiltinNeedsACommand(t *testing.T) {
	isolatePluginEnv(t)
	_, errOut, code := runDispatch(t, "--builtin")
	if code != 2 || !strings.Contains(errOut, "--builtin requires a built-in command name") {
		t.Fatalf("--builtin alone: exit %d, stderr %q", code, errOut)
	}
}
