package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// Group 6 (dialogs half): every returned path joins the fs allow-list.

func TestDialogsReturnedPathsBecomeReadable(t *testing.T) {
	b, shell := newTestBattery(t)
	dir := t.TempDir()
	picked := filepath.Join(dir, "picked.txt")
	if err := os.WriteFile(picked, []byte("picked content"), 0o600); err != nil {
		t.Fatal(err)
	}
	shell.openFileResult = []string{picked}

	res, err := callMethod(t, b, "dialogs", "openFile", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	paths := res.(dialogPathsOutput).Paths
	if len(paths) != 1 || paths[0] != picked {
		t.Fatalf("openFile = %+v", paths)
	}
	// The returned path is now on the allow-list and readable through fs.
	data, err := callMethod(t, b, "fs", "readText", `{"path":`+quoteJSON(picked)+`}`)
	if err != nil {
		t.Fatalf("dialog-returned path not readable: %v", err)
	}
	if data.(fsTextOutput).Text != "picked content" {
		t.Fatalf("content = %q", data.(fsTextOutput).Text)
	}
}

func TestDialogsCancelIsCancelledCode(t *testing.T) {
	b, shell := newTestBattery(t)
	shell.saveFileErr = ErrCancelled
	_, err := callMethod(t, b, "dialogs", "saveFile", `{"defaultName":"x.txt"}`)
	if de, ok := err.(*Error); !ok || de.Code != CodeCancelled {
		t.Fatalf("err = %v, want cancelled", err)
	}
}

// quoteJSON marshals s into a JSON string literal.
func quoteJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestDialogsAnswerTheCleanPath(t *testing.T) {
	b, shell := newTestBattery(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "picked.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	shell.openFileResult = []string{dir + "/./picked.txt"}
	res, err := callMethod(t, b, "dialogs", "openFile", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	if paths := res.(dialogPathsOutput).Paths; len(paths) != 1 || paths[0] != filepath.Join(dir, "picked.txt") {
		t.Fatalf("openFile answered %q, want the clean form fs accepts back", paths)
	}
}

func TestFolderListIsCapped(t *testing.T) {
	dir := t.TempDir()
	for i := range fsListLimit + 1 {
		if err := os.WriteFile(filepath.Join(dir, strconv.Itoa(i)), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	out, ferr := listFolder(root, ".")
	if ferr != nil || len(out.Entries) != fsListLimit || !out.Truncated {
		t.Fatalf("list of %d entries = %d entries, truncated=%v, err=%v", fsListLimit+1, len(out.Entries), out.Truncated, ferr)
	}
}
