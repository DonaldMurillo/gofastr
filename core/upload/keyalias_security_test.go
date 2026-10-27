package upload_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/upload"
)

// A key that is not in its clean form names the file of the key that is:
// "a//b.png", "a/./b.png" and "/a/b.png" all resolved to a/b.png, because
// the backend cleaned the key instead of refusing it. Erasure deletes the
// keys a user's rows name, so a row naming an alias of another user's key
// deleted that user's file. Every operation refuses such a key.
func TestLocalAliasKeysRefused(t *testing.T) {
	t.Parallel()
	ls := newTestLocalStorage(t, t.TempDir())
	ctx := context.Background()
	if err := ls.Save(ctx, "a/b.png", strings.NewReader("owner")); err != nil {
		t.Fatal(err)
	}
	for _, alias := range []string{"a//b.png", "a/./b.png", "./a/b.png", "a/b.png/", "/a/b.png", "//a/b.png", "."} {
		if err := ls.Delete(ctx, alias); !errors.Is(err, upload.ErrInvalidKey) {
			t.Errorf("Delete(%q) = %v, want ErrInvalidKey", alias, err)
		}
		if _, err := ls.Get(ctx, alias); !errors.Is(err, upload.ErrInvalidKey) {
			t.Errorf("Get(%q) = %v, want ErrInvalidKey", alias, err)
		}
		if _, err := ls.GetRange(ctx, alias); !errors.Is(err, upload.ErrInvalidKey) {
			t.Errorf("GetRange(%q) = %v, want ErrInvalidKey", alias, err)
		}
		if _, err := ls.Exists(ctx, alias); !errors.Is(err, upload.ErrInvalidKey) {
			t.Errorf("Exists(%q) = %v, want ErrInvalidKey", alias, err)
		}
		if err := ls.Save(ctx, alias, strings.NewReader("other")); !errors.Is(err, upload.ErrInvalidKey) {
			t.Errorf("Save(%q) = %v, want ErrInvalidKey", alias, err)
		}
	}
	if ok, err := ls.Exists(ctx, "a/b.png"); err != nil || !ok {
		t.Errorf("a/b.png after the aliases = %v, %v; want it kept", ok, err)
	}
}

// On a case-insensitive filesystem "A/b.png" opens a/b.png. Save already
// refuses the second spelling; Delete removed the other key's file.
func TestLocalFoldedDeleteRefused(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	ls := newTestLocalStorage(t, base)
	ctx := context.Background()
	if err := ls.Save(ctx, "a/b.png", strings.NewReader("owner")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(base, "A", "b.png")); err != nil {
		t.Skip("case-sensitive filesystem: A/b.png is another file")
	}
	if err := ls.Delete(ctx, "A/b.png"); !errors.Is(err, upload.ErrInvalidKey) {
		t.Errorf("Delete(A/b.png) = %v, want ErrInvalidKey", err)
	}
	if ok, err := ls.Exists(ctx, "a/b.png"); err != nil || !ok {
		t.Errorf("a/b.png after Delete(A/b.png) = %v, %v; want it kept", ok, err)
	}
	if err := ls.Delete(ctx, "a/b.png"); err != nil {
		t.Errorf("Delete of the stored spelling: %v", err)
	}
}

// Every key the framework mints is already clean, so the refusal never
// fails a save the framework itself makes.
func TestMintedKeysAreClean(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"report.pdf", "../../etc/passwd", "/abs/x.png", "a//b.png", ".hidden", "", "./.", "dir/"} {
		key := upload.UniqueFilename(name)
		if filepath.ToSlash(filepath.Clean(key)) != key || strings.HasPrefix(key, "/") {
			t.Errorf("UniqueFilename(%q) = %q, not in clean form", name, key)
		}
	}
}
