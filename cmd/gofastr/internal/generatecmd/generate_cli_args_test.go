package generatecmd

import (
	"os/exec"
	"strings"
	"testing"
)

// runCLICode runs the CLI and returns its combined output and exit code.
func runCLICode(t *testing.T, bin string, env []string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if exitErr, ok := err.(*exec.ExitError); ok {
		return string(out), exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("%v: %v\n%s", args, err, out)
	}
	return string(out), 0
}

// An argument left after a verb's flags is refused before any request.
// The flag package stops at the first positional, so `--published false
// --views 3` read false as an argument: the patch sent published=true,
// dropped --views 3, and exited 0.
func TestCLIRefusesStrayArguments(t *testing.T) {
	bin, env := buildFixtureCLI(t, cliMutationFixtureYAML)
	rec := newWireRecorder(t)
	env = append(env, "MYAPP_URL="+rec.URL)
	for _, args := range [][]string{
		{"gadgets", "patch", "rec-1", "--published", "false", "--views", "3"},
		{"gadgets", "create", "--title", "x", "extra"},
		{"gadgets", "list", "--title", "x", "stray", "-o", "table"},
		{"gadgets", "get", "rec-1", "rec-2"},
		{"gadgets", "delete", "rec-1", "rec-2"},
		{"gadgets", "batch-create", "--json", "[]", "extra"},
		{"gadgets", "watch", "extra"},
	} {
		out, code := runCLICode(t, bin, env, args...)
		if code != 2 {
			t.Errorf("%s exited %d, want 2:\n%s", strings.Join(args, " "), code, out)
		}
		if !strings.Contains(out, "unexpected argument") {
			t.Errorf("%s did not name the stray argument:\n%s", strings.Join(args, " "), out)
		}
		if _, _, hits := rec.last(); hits != 0 {
			t.Fatalf("%s sent a request", strings.Join(args, " "))
		}
	}
	if out, _ := runCLICode(t, bin, env, "gadgets", "patch", "rec-1", "--published", "false"); !strings.Contains(out, "--flag=false") {
		t.Errorf("a stray bool value did not point at the = form:\n%s", out)
	}
	// batch-delete takes its ids after the flags too.
	if out, code := runCLICode(t, bin, env, "gadgets", "batch-delete", "--url", rec.URL, "a", "b"); code == 2 {
		t.Errorf("batch-delete with trailing ids refused:\n%s", out)
	}
}
