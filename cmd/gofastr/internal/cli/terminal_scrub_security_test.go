package cli_test

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/agentscmd"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/migratecmd"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/validatecmd"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// payload is a directory name a cloned repo can carry: an OSC 52
// clipboard write, a screen clear, the 8-bit CSI spelling, a bidi
// override and a zero-width space. Every byte is legal in a Linux file
// name.
const payload = "evil\x1b]52;c;cHduZWQ=\x07\x1b[2J\u009b31m\u202edcba\u200b"

// captureOutput runs fn with os.Stdout and os.Stderr each sent to a
// file, and returns what landed in both.
func captureOutput(t *testing.T, fn func()) string {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	f, err := os.CreateTemp(t.TempDir(), "out-*")
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = f, f
	func() {
		defer func() { os.Stdout, os.Stderr = oldOut, oldErr }()
		clitest.CovT_capExit(t, fn)
	}()
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(f)
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// assertInert fails when out carries anything a terminal acts on rather
// than prints: C0 controls but tab and newline, DEL, C1, bidi and
// zero-width runes, and an invalid byte (a bare 0x9b is the 8-bit CSI).
func assertInert(t *testing.T, what, out string) {
	t.Helper()
	if !strings.Contains(out, "cHduZWQ=") {
		t.Fatalf("setup broken: %s printed no part of the payload, so the scrub was not exercised:\n%q", what, out)
	}
	for i, r := range out {
		if r == '\n' || r == '\t' {
			continue
		}
		if textsafe.IsUnsafe(r) {
			t.Errorf("SECURITY: [cli-terminal-escape] %s printed %U at byte %d live to the terminal:\n%q", what, r, i, out)
			return
		}
	}
	for i := 0; i < len(out); i++ {
		if out[i] == 0x9b {
			t.Errorf("SECURITY: [cli-terminal-escape] %s printed a bare 0x9b (8-bit CSI) at byte %d:\n%q", what, i, out)
			return
		}
	}
}

// Property: a repo-derived path or value that a subcommand prints through
// the shared helpers (cli.Info/Warn/Fail/Success) reaches the terminal
// inert. Owner decision (2026-11 bug bash): the helpers scrub every
// argument; their own styling, which comes from the helper and never from
// an argument, still works. Before it, each command scrubbed (or forgot
// to) on its own, and a cloned repo's directory name drove the
// terminal of whoever ran validate, migrate generate or agents sync
// on it.
func TestSubcommandsPrintRepoValuesInert(t *testing.T) {
	old := cli.StdoutIsTTY
	cli.StdoutIsTTY = false // no helper styling: every escape would be the payload's
	t.Cleanup(func() { cli.StdoutIsTTY = old })
	base := t.TempDir()
	evil := filepath.Join(base, payload)
	if err := os.Mkdir(evil, 0o755); err != nil {
		t.Skipf("file system refuses the payload name: %v", err)
	}
	clitest.CovT_chdir(t, base)

	t.Run("validate", func(t *testing.T) {
		// No gofastr.yml in it: the failure names the path.
		out := captureOutput(t, func() { validatecmd.RunValidate([]string{payload}) })
		assertInert(t, "gofastr validate", out)
	})
	t.Run("agents sync", func(t *testing.T) {
		// No AGENTS.md in it: the failure names the file.
		out := captureOutput(t, func() { agentscmd.RunAgents([]string{"sync", payload}) })
		assertInert(t, "gofastr agents sync", out)
	})
	t.Run("migrate generate", func(t *testing.T) {
		spec := filepath.Join(base, "gofastr.yml")
		if err := os.WriteFile(spec, []byte("app:\n  name: testapp\nentities:\n  - name: posts\n    table: posts\n    fields:\n      - name: title\n        type: string\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		out := captureOutput(t, func() {
			migratecmd.RunMigrate([]string{"generate", "create_posts", "--from=" + spec, "--migrations=" + filepath.Join(payload, "migrations"), "--snapshot=" + filepath.Join(payload, "snap.json")})
		})
		assertInert(t, "gofastr migrate generate", out)
	})
}

// The helpers' own styling is theirs, not an argument's: on a terminal
// the mark keeps its colour, and an argument carrying the same escape is
// printed without it.
func TestHelpersKeepTheirOwnStyling(t *testing.T) {
	old := cli.StdoutIsTTY
	cli.StdoutIsTTY = true
	t.Cleanup(func() { cli.StdoutIsTTY = old })
	for name, call := range map[string]func(){
		"Success": func() { cli.Success("%s", "\x1b[31mred\x1b[0m") },
		"Fail":    func() { cli.Fail("%v", "\x1b[31mred\x1b[0m") },
		"Info":    func() { cli.Info("%s", "\x1b[31mred\x1b[0m") },
		"Warn":    func() { cli.Warn("%s", "\x1b[31mred\x1b[0m") },
	} {
		out := captureOutput(t, call)
		if strings.Count(out, "\x1b") != 2 || !strings.Contains(out, "\x1b[0m [31mred[0m\n") {
			t.Errorf("%s: want the helper's mark styled and the argument's escapes stripped, got %q", name, out)
		}
	}
	if got := cli.InfoString("%s", "a\x1b]52;c;x\x07b"); strings.Count(got, "\x1b") != 2 || !strings.HasSuffix(got, "\x1b[0m a]52;c;xb") {
		t.Errorf("InfoString: %q", got)
	}
}

// Each argument keeps its verb, width and flags: %q still escapes (so it
// shows the ESC as \x1b rather than dropping it), %5s pads, %x encodes,
// a %*d width stays an int, and an error reads through %v. Newline and
// tab in an argument survive; a carriage return does not.
func TestHelperArgumentsKeepTheirVerbs(t *testing.T) {
	old := cli.StdoutIsTTY
	cli.StdoutIsTTY = false
	t.Cleanup(func() { cli.StdoutIsTTY = old })
	got := cli.InfoString("%q|%5s|%x|%*d|%v|%s", "a\x1bb", "z", "ab", 3, 7, os.ErrNotExist, "l1\n\tl2\rX")
	want := `  → "a\x1bb"|    z|6162|  7|file does not exist|l1` + "\n\tl2X"
	if got != want {
		t.Errorf("InfoString = %q\nwant         %q", got, want)
	}
}
