package storage

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Keys that are perfectly legal on Unix but not portable to Windows. They
// were accepted before the Windows-portability rules landed, so real
// deployments have objects stored under them.
var unportableButExistingKeys = []string{
	"exports/2026-08-02T12:00:00Z.json", // ISO timestamp — colons
	"user:123/avatar.png",               // namespaced key — colon
	"reports/NUL",                       // reserved Windows device name
	"archive/backup.",                   // trailing dot
}

// TestExistingKeysStayReadableAndDeletable pins the compatibility contract:
// tightening key rules must not orphan data. Save may refuse an unportable
// key, but Get/Exists/Delete must still reach an object that is already on
// disk — otherwise the tightening silently makes stored data unreachable
// AND unremovable, with no migration path.
func TestExistingKeysStayReadableAndDeletable(t *testing.T) {
	dir := t.TempDir()
	ls := NewLocalStorage(dir)
	ctx := context.Background()

	for _, key := range unportableButExistingKeys {
		// Plant the object directly, the way a pre-upgrade release would have.
		full := filepath.Join(dir, key)
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatalf("%s: mkdir: %v", key, err)
		}
		if err := os.WriteFile(full, []byte("payload-"+key), 0o600); err != nil {
			t.Skipf("%s: host filesystem cannot represent this key: %v", key, err)
		}

		ok, err := ls.Exists(ctx, key)
		if err != nil {
			t.Errorf("Exists(%q) = error %v; existing objects must stay reachable", key, err)
			continue
		}
		if !ok {
			t.Errorf("Exists(%q) = false; the object is on disk", key)
			continue
		}
		rc, err := ls.Get(ctx, key)
		if err != nil {
			t.Errorf("Get(%q) = error %v; existing objects must stay readable", key, err)
			continue
		}
		got := new(bytes.Buffer)
		_, _ = got.ReadFrom(rc)
		rc.Close()
		if got.String() != "payload-"+key {
			t.Errorf("Get(%q) = %q", key, got.String())
		}
		if err := ls.Delete(ctx, key); err != nil {
			t.Errorf("Delete(%q) = error %v; existing objects must stay removable", key, err)
		}
	}
}

// TestSaveStillRejectsUnportableKeys keeps the forward-looking half: new
// writes stay portable, so a store written on Unix can be served on Windows.
func TestSaveStillRejectsUnportableKeys(t *testing.T) {
	ls := NewLocalStorage(t.TempDir())
	ctx := context.Background()
	for _, key := range unportableButExistingKeys {
		if err := ls.Save(ctx, key, bytes.NewReader([]byte("x"))); err == nil {
			t.Errorf("Save(%q) succeeded; new keys must stay Windows-portable", key)
		}
	}
}

// TestTraversalRejectedOnEveryOperation — the security rules are NOT
// write-only. They must hold on every code path.
func TestTraversalRejectedOnEveryOperation(t *testing.T) {
	ls := NewLocalStorage(t.TempDir())
	ctx := context.Background()
	for _, key := range []string{"../escape", "a/../../escape", "/etc/passwd", `\windows\system32`} {
		if _, err := ls.Exists(ctx, key); err == nil {
			t.Errorf("Exists(%q) allowed a traversal key", key)
		}
		if _, err := ls.Get(ctx, key); err == nil {
			t.Errorf("Get(%q) allowed a traversal key", key)
		}
		if err := ls.Delete(ctx, key); err == nil {
			t.Errorf("Delete(%q) allowed a traversal key", key)
		}
		if err := ls.Save(ctx, key, bytes.NewReader([]byte("x"))); err == nil {
			t.Errorf("Save(%q) allowed a traversal key", key)
		}
	}
}
