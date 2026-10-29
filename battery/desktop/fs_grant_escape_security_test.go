package desktop

import (
	"os"
	"path/filepath"
	"testing"
)

// fsWriteDenied writes text to the raw path p (never re-cleaned here)
// and fails the test unless the battery answers denied.
func fsWriteDenied(t *testing.T, b *Battery, p string) {
	t.Helper()
	_, err := callMethod(t, b, "fs", "writeText", `{"path":`+quoteJSON(p)+`,"text":"x"}`)
	if de, ok := err.(*Error); !ok || de.Code != CodeDenied {
		t.Errorf("writeText %q: err = %v, want denied", p, err)
	}
}

// A grant on a file that does not exist yet (a save target) let a write
// out of the granted directory. pathAllowed checked the cleaned path,
// then resolved the RAW path and passed whenever that resolution failed,
// which it always does for a missing leaf. A raw ".." past a symlinked
// directory cleans to the grant while the kernel follows the link, and a
// directory swapped for a symlink after the grant resolves nowhere.
func TestFSWriteStaysInsideTheGrant(t *testing.T) {
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	allow, out := filepath.Join(d, "allow"), filepath.Join(d, "out")
	for _, p := range []string{allow, filepath.Join(out, "sub")} {
		if err := os.MkdirAll(p, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(out, "sub"), filepath.Join(allow, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	b, shell := newTestBattery(t)
	note := filepath.Join(allow, "note.txt")
	later := filepath.Join(allow, "later", "n.txt")
	for _, p := range []string{note, later} {
		if err := b.AllowPath(p); err != nil {
			t.Fatal(err)
		}
	}

	// Cleans to the grant; the kernel resolves link/.. to out.
	fsWriteDenied(t, b, allow+"/link/../note.txt")
	if _, err := os.Stat(filepath.Join(out, "note.txt")); err == nil {
		t.Error("the write landed outside the granted directory")
	}
	// Any other unclean spelling of the grant is refused too.
	fsWriteDenied(t, b, allow+"//note.txt")

	// The granted file's parent did not exist at grant time and is now a
	// symlink out of the directory.
	if err := os.Symlink(out, filepath.Join(allow, "later")); err != nil {
		t.Fatal(err)
	}
	fsWriteDenied(t, b, later)
	if _, err := os.Stat(filepath.Join(out, "n.txt")); err == nil {
		t.Error("the write followed a parent swapped for a symlink")
	}

	// fs refuses unclean spellings, so a dialog hands the page the clean
	// form it granted, and that form round-trips.
	shell.saveFileResult = allow + "//saved.txt"
	res, err := callMethod(t, b, "dialogs", "saveFile", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	saved := res.(dialogPathOutput).Path
	if saved != filepath.Join(allow, "saved.txt") {
		t.Fatalf("saveFile = %q, want the clean form", saved)
	}
	if _, err := callMethod(t, b, "fs", "writeText", `{"path":`+quoteJSON(saved)+`,"text":"ok"}`); err != nil {
		t.Fatalf("writeText on the dialog's path: %v", err)
	}
	shell.openFileResult = []string{allow + "/./saved.txt"}
	res, err = callMethod(t, b, "dialogs", "openFile", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	if got := res.(dialogPathsOutput).Paths; len(got) != 1 || got[0] != saved {
		t.Fatalf("openFile = %q, want [%q]", got, saved)
	}
	shell.folderResult = allow + "/"
	res, err = callMethod(t, b, "dialogs", "openFolder", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	if got := res.(dialogPathOutput).Path; got != allow {
		t.Fatalf("openFolder = %q, want %q", got, allow)
	}
	if _, err := callMethod(t, b, "fs", "stat", `{"path":`+quoteJSON(allow)+`}`); err != nil {
		t.Fatalf("stat on the folder's path: %v", err)
	}

	// The grant itself still works.
	if _, err := callMethod(t, b, "fs", "writeText", `{"path":`+quoteJSON(note)+`,"text":"ok"}`); err != nil {
		t.Fatalf("writeText on the granted path: %v", err)
	}
}
