package desktop

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Group 6: the fs allow-list and dialogs interplay. Handlers are
// invoked directly (the chokepoint tests cover the transport).

// callMethod looks a method up in the battery's registry and invokes
// its handler directly.
func callMethod(t *testing.T, b *Battery, capName, method, input string) (any, error) {
	t.Helper()
	cap, ok := b.reg.lookup(capName)
	if !ok {
		t.Fatalf("no capability %q", capName)
	}
	for _, m := range cap.Methods {
		if m.Name == method {
			return m.Handler(context.Background(), json.RawMessage(input))
		}
	}
	t.Fatalf("no method %q on %q", method, capName)
	return nil, nil
}

func TestFSPathOutsideAllowListRefused(t *testing.T) {
	b, _ := newTestBattery(t)
	other := filepath.Join(t.TempDir(), "other.txt")
	if err := os.WriteFile(other, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := callMethod(t, b, "fs", "readText", `{"path":`+quoteJSON(other)+`}`)
	if err == nil {
		t.Fatal("read outside allow-list succeeded")
	}
	if de, ok := err.(*Error); !ok || de.Code != CodeDenied {
		t.Fatalf("err = %v, want denied", err)
	}
	// The refusal never echoes the path.
	if strings.Contains(err.Error(), other) {
		t.Fatalf("denial echoes the path: %v", err)
	}
}

func TestFSDotDotTraversalRefused(t *testing.T) {
	b, _ := newTestBattery(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(target, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := b.AllowPath(filepath.Join(dir, "start.txt")); err != nil {
		t.Fatal(err)
	}
	// A traversal from the allow-listed clean path never matches.
	trav := filepath.Join(dir, "start.txt", "..", "target.txt")
	_, err := callMethod(t, b, "fs", "readText", `{"path":`+quoteJSON(trav)+`}`)
	if err == nil {
		t.Fatal("traversal read succeeded")
	}
}

func TestFSSymlinkSwappedAfterAllowPathRefused(t *testing.T) {
	dir := t.TempDir()
	real1 := filepath.Join(dir, "real1.txt")
	real2 := filepath.Join(dir, "real2.txt")
	for _, p := range []string{real1, real2} {
		if err := os.WriteFile(p, []byte("data"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(real1, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	b, _ := newTestBattery(t)
	if err := b.AllowPath(link); err != nil {
		t.Fatal(err)
	}
	// Before the swap: readable.
	if _, err := callMethod(t, b, "fs", "readText", `{"path":`+quoteJSON(link)+`}`); err != nil {
		t.Fatalf("read through symlink before swap: %v", err)
	}
	// Swap the symlink to a different target AFTER AllowPath.
	os.Remove(link)
	if err := os.Symlink(real2, link); err != nil {
		t.Fatal(err)
	}
	_, err := callMethod(t, b, "fs", "readText", `{"path":`+quoteJSON(link)+`}`)
	if err == nil {
		t.Fatal("read through swapped symlink succeeded — allow-list widened")
	}
}

func TestFSWriteCreates0600AndRoundTrips(t *testing.T) {
	b, _ := newTestBattery(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "note.txt")
	if err := b.AllowPath(target); err != nil {
		t.Fatal(err)
	}
	if _, err := callMethod(t, b, "fs", "writeText", `{"path":`+quoteJSON(target)+`,"text":"hello"}`); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("written file mode = %o, want 0600", perm)
	}
	// Overwrite keeps 0600.
	if _, err := callMethod(t, b, "fs", "writeText", `{"path":`+quoteJSON(target)+`,"text":"again"}`); err != nil {
		t.Fatal(err)
	}
	info, _ = os.Stat(target)
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("overwritten file mode = %o, want 0600", perm)
	}
	res, err := callMethod(t, b, "fs", "readText", `{"path":`+quoteJSON(target)+`}`)
	if err != nil {
		t.Fatal(err)
	}
	if out := res.(fsTextOutput); out.Text != "again" {
		t.Fatalf("round-trip = %q", out.Text)
	}
	// stat works too.
	st, err := callMethod(t, b, "fs", "stat", `{"path":`+quoteJSON(target)+`}`)
	if err != nil {
		t.Fatal(err)
	}
	if st.(fsStatOutput).Size != 5 || st.(fsStatOutput).IsDir {
		t.Fatalf("stat = %+v", st)
	}
	// No leftover temp files in the directory.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("stray files after write: %v", entries)
	}
}

func TestFSOversizeReadRefused(t *testing.T) {
	b, _ := newTestBattery(t)
	dir := t.TempDir()
	big := filepath.Join(dir, "big.txt")
	if err := os.WriteFile(big, make([]byte, fsReadLimit+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := b.AllowPath(big); err != nil {
		t.Fatal(err)
	}
	_, err := callMethod(t, b, "fs", "readText", `{"path":`+quoteJSON(big)+`}`)
	if de, ok := err.(*Error); !ok || de.Code != CodeInvalidInput {
		t.Fatalf("err = %v, want invalid_input", err)
	}
}

func TestFSAllowPathRequiresAbsolute(t *testing.T) {
	b, _ := newTestBattery(t)
	if err := b.AllowPath("relative/path.txt"); err == nil {
		t.Fatal("relative path accepted")
	}
}
