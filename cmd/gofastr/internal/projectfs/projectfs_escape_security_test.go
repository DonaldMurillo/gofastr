package projectfs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// hostileName is a directory name a cloned repo can carry: an OSC 52
// clipboard write, a screen clear, the 8-bit CSI spelling and a bidi
// override. Every byte is legal in a Linux file name.
const hostileName = "d\x1b]52;c;cHduZWQ=\x07\x1b[2J\u009b31m\u202e"

// The refusal Resolve returns names the link, and the link's path is
// made of names from the checkout: a directory on the way, or a
// component Resolve took from another link's target text. The error
// is printed to the terminal by every command that hits it, so it must
// carry the name inert: quoted, never raw.
func TestOutOfProjectErrorCarriesNoLiveEscapes(t *testing.T) {
	proj := t.TempDir()
	evil := filepath.Join(proj, hostileName)
	if err := os.Mkdir(evil, 0o755); err != nil {
		t.Skipf("file system refuses the payload name: %v", err)
	}
	if err := os.Symlink("/nonexistent/elsewhere", filepath.Join(evil, "l")); err != nil {
		t.Fatal(err)
	}
	// t -> <hostile>/l: the hostile component comes from t's target text.
	if err := os.Symlink(hostileName+"/l", filepath.Join(proj, "t")); err != nil {
		t.Fatal(err)
	}
	// u -> <hostile>/../../x: the climb out is refused naming u.
	if err := os.Symlink(hostileName+"/../../x", filepath.Join(proj, "u")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{hostileName + "/l/f", "t/f", "u"} {
		err := Check(proj, name)
		if err == nil {
			t.Fatalf("Check(%q) passed a link out of the project", name)
		}
		msg := err.Error()
		if !strings.Contains(msg, "symlink out of the project") {
			t.Fatalf("Check(%q): unexpected error %q", name, msg)
		}
		for i, r := range msg {
			if r != '\n' && r != '\t' && textsafe.IsUnsafe(r) {
				t.Errorf("SECURITY: [projectfs-error-escape] Check(%q) error carries %U at byte %d live:\n%q", name, r, i, msg)
				break
			}
		}
	}
}
