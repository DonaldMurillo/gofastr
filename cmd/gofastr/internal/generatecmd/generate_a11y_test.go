package generatecmd

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

func TestGenerateScreenDispatches(t *testing.T) {
	dir := clitest.ScaffoldEmptyDir(t)
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() { RunGenerate([]string{"screen", "contact"}) })
	})
	if code != -1 {
		t.Fatalf("`generate screen contact` exited %d", code)
	}
	if !clitest.FileExists(dir, "screen_contact.go") {
		t.Fatal("screen_contact.go not written")
	}
}

func TestGenerateScreenFromA11yErrors(t *testing.T) {
	dir := clitest.ScaffoldEmptyDir(t)
	bad := filepath.Join(dir, "bad.yml")
	if err := os.WriteFile(bad, []byte("- button \"Save"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"screen", "x", "--from-a11y=" + bad},
		{"screen", "x", "--from-a11y=" + filepath.Join(dir, "missing.yml")},
		{"screen", "x", "--from-a11y"},
		{"entity", "x", "--from-a11y=" + bad},
	} {
		code := clitest.CovT_capExit(t, func() {
			clitest.CovT_capStdout(t, func() { RunGenerate(args) })
		})
		if code != 1 {
			t.Errorf("%v: exit %d, want 1", args, code)
		}
	}
	if clitest.FileExists(dir, "screen_x.go") {
		t.Error("a failed snapshot read still wrote screen_x.go")
	}
}

// a11yRun runs `gofastr generate` with stdout and stderr captured, both
// kept when the command exits.
func a11yRun(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	outF, err := os.CreateTemp(t.TempDir(), "out-*")
	if err != nil {
		t.Fatal(err)
	}
	errF, err := os.CreateTemp(t.TempDir(), "err-*")
	if err != nil {
		t.Fatal(err)
	}
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outF, errF
	code = clitest.CovT_capExit(t, func() { RunGenerate(args) })
	os.Stdout, os.Stderr = oldOut, oldErr
	read := func(f *os.File) string {
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(f)
		if err != nil {
			t.Fatal(err)
		}
		f.Close()
		return string(b)
	}
	return code, read(outF), read(errF)
}

func TestFromA11yJSONStaysParseable(t *testing.T) {
	dir := clitest.ScaffoldEmptyDir(t)
	bad := filepath.Join(dir, "bad.yml")
	if err := os.WriteFile(bad, []byte("- button \"Save"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, _ := a11yRun(t, "screen", "x", "--from-a11y="+bad, "--dry-run", "--json")
	var doc struct{ Errors []any }
	if code != 1 || json.Unmarshal([]byte(out), &doc) != nil || len(doc.Errors) == 0 {
		t.Errorf("exit %d, stdout not the JSON error shape:\n%s", code, out)
	}
	good := filepath.Join(dir, "good.yml")
	if err := os.WriteFile(good, []byte("- heading \"Hi\" [level=1]\n- img \"Logo\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := a11yRun(t, "screen", "x", "--from-a11y="+good, "--dry-run", "--json")
	if code != -1 || !json.Valid([]byte(out)) || !strings.Contains(errOut, "warning: skipped img") {
		t.Errorf("exit %d; stdout %q; stderr %q; want JSON out and the warning on stderr", code, out, errOut)
	}
}
