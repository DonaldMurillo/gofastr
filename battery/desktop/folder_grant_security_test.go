package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// A picked folder grants read and list on everything under it, through
// an os.Root at the folder: a symlink or ".." inside it cannot reach a
// file outside, and no path under it becomes writable.
func TestFolderPickGrantsReadUnderIt(t *testing.T) {
	b, shell := newTestBattery(t)
	folder := filepath.Join(t.TempDir(), "picked")
	outside := t.TempDir()
	mustWrite(t, filepath.Join(folder, "child.txt"), "private")
	mustWrite(t, filepath.Join(folder, "sub", "deep.txt"), "deep")
	mustWrite(t, filepath.Join(outside, "secret.txt"), "secret")
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(folder, "out")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(folder, "updir")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("child.txt", filepath.Join(folder, "alias")); err != nil {
		t.Fatal(err)
	}
	shell.folderResult = folder
	if _, err := callMethod(t, b, "dialogs", "openFolder", `{}`); err != nil {
		t.Fatal(err)
	}

	for path, want := range map[string]string{
		filepath.Join(folder, "child.txt"):       "private",
		filepath.Join(folder, "sub", "deep.txt"): "deep",
		filepath.Join(folder, "alias"):           "private",
	} {
		res, err := callMethod(t, b, "fs", "readText", `{"path":`+quoteJSON(path)+`}`)
		if err != nil || res.(fsTextOutput).Text != want {
			t.Errorf("readText %s = %v, %v; want %q", path, res, err, want)
		}
	}
	for _, path := range []string{folder, filepath.Join(folder, "sub", "deep.txt")} {
		if _, err := callMethod(t, b, "fs", "stat", `{"path":`+quoteJSON(path)+`}`); err != nil {
			t.Errorf("stat %s: %v", path, err)
		}
	}
	res, err := callMethod(t, b, "fs", "list", `{"path":`+quoteJSON(folder)+`}`)
	if err != nil {
		t.Fatalf("list the picked folder: %v", err)
	}
	got, _ := json.Marshal(res)
	want := `{"entries":[{"name":"alias","isDir":false},{"name":"child.txt","isDir":false},` +
		`{"name":"out","isDir":false},{"name":"sub","isDir":true},{"name":"updir","isDir":false}],"truncated":false}`
	if string(got) != want {
		t.Errorf("list = %s\nwant   %s", got, want)
	}
	if _, err := callMethod(t, b, "fs", "list", `{"path":`+quoteJSON(filepath.Join(folder, "sub"))+`}`); err != nil {
		t.Errorf("list a folder under the pick: %v", err)
	}

	// Out of the folder: through a symlink, a "..", an unclean spelling,
	// or a sibling. Each is the fixed denial.
	denied := map[string][]string{
		"readText": {
			filepath.Join(folder, "out"),
			filepath.Join(folder, "updir", "secret.txt"),
			folder + "/sub/../child.txt",
			filepath.Join(filepath.Dir(folder), "sibling.txt"),
		},
		"stat": {filepath.Join(folder, "updir", "secret.txt"), filepath.Dir(folder)},
		"list": {filepath.Join(folder, "updir"), filepath.Dir(folder), outside},
	}
	mustWrite(t, filepath.Join(filepath.Dir(folder), "sibling.txt"), "sibling")
	for method, paths := range denied {
		for _, path := range paths {
			_, err := callMethod(t, b, "fs", method, `{"path":`+quoteJSON(path)+`}`)
			if refusal, ok := err.(*Error); !ok || refusal.Code != CodeDenied {
				t.Errorf("%s %s = %v, want denied", method, path, err)
			}
		}
	}

	// Read and list only: a write needs the exact path from a file dialog.
	child := filepath.Join(folder, "child.txt")
	for _, path := range []string{child, filepath.Join(folder, "new.txt")} {
		_, err := callMethod(t, b, "fs", "writeText", `{"path":`+quoteJSON(path)+`,"text":"changed"}`)
		if refusal, ok := err.(*Error); !ok || refusal.Code != CodeDenied {
			t.Errorf("writeText %s = %v, want denied", path, err)
		}
	}
	if data, err := os.ReadFile(child); err != nil || string(data) != "private" {
		t.Fatalf("child changed: %q (%v)", data, err)
	}

	// The picked path swapped for a link to another folder since the pick
	// names that other folder: refused.
	if err := os.Rename(folder, folder+".moved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, folder); err != nil {
		t.Fatal(err)
	}
	_, err = callMethod(t, b, "fs", "readText", `{"path":`+quoteJSON(filepath.Join(folder, "secret.txt"))+`}`)
	if refusal, ok := err.(*Error); !ok || refusal.Code != CodeDenied {
		t.Errorf("readText through a swapped folder = %v, want denied", err)
	}
}

// fs.list answers only under a picked folder: an exact file or folder
// path from AllowPath does not list.
func TestListNeedsAFolderPick(t *testing.T) {
	b, _ := newTestBattery(t)
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.txt"), "a")
	if err := b.AllowPath(dir); err != nil {
		t.Fatal(err)
	}
	_, err := callMethod(t, b, "fs", "list", `{"path":`+quoteJSON(dir)+`}`)
	if refusal, ok := err.(*Error); !ok || refusal.Code != CodeDenied {
		t.Fatalf("list on an exact grant = %v, want denied", err)
	}
}

func mustWrite(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}
