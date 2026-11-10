//go:build unix

package update

import (
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// withUmask runs the test body under mask. Under the usual 022 a create
// call already yields 0755, so a mode bug only shows under a tighter one.
// The umask is process-wide: callers must not run in parallel.
func withUmask(t *testing.T, mask int) {
	t.Helper()
	old := syscall.Umask(mask)
	t.Cleanup(func() { syscall.Umask(old) })
}

func TestCopyTreeModeUnderTightUmask(t *testing.T) {
	withUmask(t, 0o077)
	src := filepath.Join(t.TempDir(), "source")
	if err := os.WriteFile(src, []byte("bin"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(src, 0o755); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "copy")
	if err := copyTree(src, dst); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("copied mode %o, want 755", info.Mode().Perm())
	}
}

func TestExtractModeUnderTightUmask(t *testing.T) {
	withUmask(t, 0o077)
	data := buildZip(t, map[string]struct {
		content []byte
		mode    fs.FileMode
	}{
		"Notes.app/":    {nil, fs.ModeDir | 0o755},
		"Notes.app/bin": {[]byte("bin"), 0o755},
	})
	app, err := ExtractZip(data, t.TempDir(), "Notes.app")
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(app, "bin"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("extracted mode %o, want 755", info.Mode().Perm())
	}
}

// A zip built on Windows or by a careless tool records 0777. Group and
// other write on a file in the installed bundle lets any local user
// swap the binary the next launch runs.
func TestExtractDropsGroupOtherWrite(t *testing.T) {
	withUmask(t, 0)
	data := buildZip(t, map[string]struct {
		content []byte
		mode    fs.FileMode
	}{
		"Notes.app/":    {nil, fs.ModeDir | 0o777},
		"Notes.app/bin": {[]byte("bin"), 0o777},
	})
	app, err := ExtractZip(data, t.TempDir(), "Notes.app")
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(app, "bin"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o022 != 0 {
		t.Fatalf("extracted file mode %o keeps group/other write", info.Mode().Perm())
	}
}

// MoveTree's copy path runs after its absence check; a destination that
// appears in between must stop the copy, not take a merge of both trees.
func TestCopyTreeRefusesExistingDestination(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "bin"), []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}
	dst := t.TempDir()
	if err := os.WriteFile(filepath.Join(dst, "bin"), []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := copyTree(src, dst); err == nil {
		t.Fatal("copyTree wrote into an existing destination")
	}
	got, _ := os.ReadFile(filepath.Join(dst, "bin"))
	if string(got) != "old" {
		t.Fatalf("existing destination overwritten: %q", got)
	}
}

// A read-only source directory copies: the destination stays writable
// while its children are copied, then takes the extractor's directory
// mode, which keeps the owner able to remove it on the next update.
func TestCopyTreeReadOnlyDirectory(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	if err := os.Mkdir(src, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "f"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(src, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(src, 0o700) })
	dst := filepath.Join(t.TempDir(), "dst")
	if err := copyTree(src, dst); err != nil {
		t.Fatalf("copyTree: %v", err)
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o755 {
		t.Fatalf("dst mode = %o, want 755", got)
	}
}

// The feed pair is public: a static web server running as another user
// reads it. The modes hold under a tight umask and over files an older
// release wrote 0600, while the signing key and the lock stay private.
func TestFeedFilesArePublic(t *testing.T) {
	withUmask(t, 0o077)
	dir := t.TempDir()
	keyPath, _, _, _ := writeKeyFile(t, dir)
	archive := writeArchive(t, dir)

	// The trailing separator is what -o dir/feed/ hands over.
	fresh := filepath.Join(dir, "grandparent", "parent", "feed")
	if err := WriteFeed(fresh+string(filepath.Separator), keyPath, "1.2.0", "", "darwin-arm64", archive, "https://example.com/a.zip", 0); err != nil {
		t.Fatal(err)
	}
	// Every directory the call made is walkable, not only the last.
	wantMode(t, filepath.Join(dir, "grandparent"), 0o755)
	wantMode(t, filepath.Join(dir, "grandparent", "parent"), 0o755)
	wantMode(t, fresh, 0o755)
	// The temp dir was there before and is not the feed's to open up.
	wantMode(t, dir, 0o700)
	wantMode(t, filepath.Join(fresh, "manifest.json"), 0o644)
	wantMode(t, filepath.Join(fresh, "manifest.json.sig"), 0o644)
	wantMode(t, filepath.Join(fresh, ".manifest.lock"), 0o600)
	wantMode(t, keyPath, 0o600)

	// A second write replaces each file by rename: a reader holding the
	// old one keeps a whole file, never a truncated one.
	before, err := os.Stat(filepath.Join(fresh, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		after, err := os.Stat(filepath.Join(fresh, "manifest.json"))
		if err == nil && os.SameFile(before, after) {
			t.Error("manifest.json was rewritten in place, not replaced")
		}
		if left, _ := filepath.Glob(filepath.Join(fresh, ".manifest.json*.tmp-*")); len(left) > 0 {
			t.Errorf("temporary files left in the served directory: %v", left)
		}
	})
	// A second write over the same files keeps them public.
	if err := os.Chmod(filepath.Join(fresh, "manifest.json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(fresh, "manifest.json.sig"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteFeed(fresh, keyPath, "1.2.0", "", "darwin-arm64", archive, "https://example.com/a.zip", 0); err != nil {
		t.Fatal(err)
	}
	wantMode(t, filepath.Join(fresh, "manifest.json"), 0o644)
	wantMode(t, filepath.Join(fresh, "manifest.json.sig"), 0o644)

	// A directory that already exists keeps the mode its owner chose.
	private := filepath.Join(dir, "private")
	if err := os.Mkdir(private, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := WriteFeed(private, keyPath, "1.2.0", "", "darwin-arm64", archive, "https://example.com/a.zip", 0); err != nil {
		t.Fatal(err)
	}
	wantMode(t, private, 0o700)
}

func wantMode(t *testing.T, path string, want fs.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("%s mode = %o, want %o", filepath.Base(path), got, want)
	}
}
