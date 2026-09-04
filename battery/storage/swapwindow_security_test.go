package storage

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestLocalStorageRefusesMidResolvePlant pins the resolution-to-syscall
// window the 2026-09-04 resolve-then-open fix left open, found by the
// same day's red-probe round; fixed by routing every LocalStorage
// syscall through an *os.Root pinned to the resolved base, so a symlink
// planted after resolution is refused by the kernel at the syscall
// instead of followed.
//
// Family: F3 path canonicalization at filesystem sinks (TOCTOU between
// the resolved containment proof and the syscalls that follow it).
// Property: proving containment on the symlink-RESOLVED chain and then
// issuing the syscall is not containment: a symlink planted into
// BaseDir AFTER the resolution must still be refused — by the kernel,
// at the syscall, not by a check that already ran.
// Surfaces: battery/storage/local.go:Save, ::open (serves Get and
// GetRange), ::Delete, ::Exists — all behind resolvePath, whose
// resolution the plant must not be able to invalidate between proof
// and use. The internal afterResolve seam drives the interleave
// deterministically; it is nil in production (tests here must not run
// in parallel while it is installed). A WithTempDir outside BaseDir
// still stages resolve-then-open (saveResolveThenOpen): os.Root.Rename
// cannot cross the root, the one operation the root cannot express.

func TestLocalStorageRefusesMidResolvePlant(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink planting and POSIX containment semantics are not portable on Windows")
	}

	base := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("OUTSIDE-SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The Delete surface needs a victim file under a swappable parent.
	outsideSub := filepath.Join(outside, "sub")
	if err := os.MkdirAll(outsideSub, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outsideSub, "victim.txt"), []byte("OUTSIDE-VICTIM"), 0o600); err != nil {
		t.Fatal(err)
	}

	install := func(plant func()) {
		afterResolve = func(string) { plant() }
	}
	t.Cleanup(func() { afterResolve = nil })

	ctx := context.Background()

	// --- Read surfaces: leaf symlink planted at a key that resolved
	// clean (it did not exist), between resolution and the open. ---
	t.Run("Get", func(t *testing.T) {
		ls := NewLocalStorage(base)
		install(func() {
			_ = os.Symlink(secret, filepath.Join(base, "victim.txt"))
		})
		defer os.Remove(filepath.Join(base, "victim.txt"))

		rc, err := ls.Get(ctx, "victim.txt")
		if err == nil {
			body, _ := io.ReadAll(rc)
			rc.Close()
			if string(body) == "OUTSIDE-SECRET" {
				t.Fatalf("SECURITY: [storage-plant-read] Get served OUTSIDE-SECRET from a symlink planted after resolution: the containment proof ran on a clean tree and the open followed a tree that was swapped underneath it.")
			}
		}
	})

	t.Run("GetRange", func(t *testing.T) {
		ls := NewLocalStorage(base)
		install(func() {
			_ = os.Symlink(secret, filepath.Join(base, "victim.txt"))
		})
		defer os.Remove(filepath.Join(base, "victim.txt"))

		rs, err := ls.GetRange(ctx, "victim.txt")
		if err == nil {
			body, _ := io.ReadAll(rs)
			rs.Close()
			if string(body) == "OUTSIDE-SECRET" {
				t.Fatalf("SECURITY: [storage-plant-read] GetRange served OUTSIDE-SECRET from a post-resolution symlink plant.")
			}
		}
	})

	t.Run("Exists", func(t *testing.T) {
		ls := NewLocalStorage(base)
		install(func() {
			_ = os.Symlink(secret, filepath.Join(base, "victim.txt"))
		})
		defer os.Remove(filepath.Join(base, "victim.txt"))

		if ok, err := ls.Exists(ctx, "victim.txt"); err == nil && ok {
			t.Fatalf("SECURITY: [storage-plant-exists] Exists answered true for a file OUTSIDE BaseDir reached through a post-resolution symlink plant: an existence oracle for arbitrary paths.")
		}
	})

	// --- Delete surface: parent directory swapped for a symlink after
	// the key resolved through the real directory. ---
	t.Run("Delete", func(t *testing.T) {
		inRootSub := filepath.Join(base, "sub")
		if err := os.MkdirAll(inRootSub, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(inRootSub, "victim.txt"), []byte("IN-ROOT"), 0o600); err != nil {
			t.Fatal(err)
		}
		ls := NewLocalStorage(base)
		install(func() {
			// Resolution has already proven the chain (it saw the real
			// directory); the plant may now reshape it freely.
			if err := os.Remove(filepath.Join(inRootSub, "victim.txt")); err != nil {
				t.Errorf("swap: remove in-root victim: %v", err)
				return
			}
			if err := os.Remove(inRootSub); err != nil {
				t.Errorf("swap: remove real sub: %v", err)
				return
			}
			if err := os.Symlink(outsideSub, inRootSub); err != nil {
				t.Errorf("swap: plant symlinked sub: %v", err)
			}
		})
		defer func() {
			_ = os.Remove(inRootSub)
			_ = os.MkdirAll(inRootSub, 0o700)
		}()

		_ = ls.Delete(ctx, "sub/victim.txt")
		if _, err := os.Stat(filepath.Join(outsideSub, "victim.txt")); err != nil {
			t.Fatalf("SECURITY: [storage-plant-delete] Delete unlinked a file OUTSIDE BaseDir: the parent directory was swapped for a symlink after resolution and the remove followed it.")
		}
	})

	// --- Save surface: same parent swap; the staged write must not
	// land outside the root. ---
	t.Run("Save", func(t *testing.T) {
		inRootSub := filepath.Join(base, "sub2")
		if err := os.MkdirAll(inRootSub, 0o700); err != nil {
			t.Fatal(err)
		}
		outsideSub2 := filepath.Join(outside, "sub2")
		if err := os.MkdirAll(outsideSub2, 0o700); err != nil {
			t.Fatal(err)
		}
		ls := NewLocalStorage(base)
		install(func() {
			if err := os.Remove(inRootSub); err != nil {
				t.Errorf("swap: remove real sub2: %v", err)
				return
			}
			if err := os.Symlink(outsideSub2, inRootSub); err != nil {
				t.Errorf("swap: plant symlinked sub2: %v", err)
			}
		})
		defer func() {
			_ = os.Remove(inRootSub)
		}()

		_ = ls.Save(ctx, "sub2/new.txt", strings.NewReader("ESCAPED"))
		if _, err := os.Stat(filepath.Join(outsideSub2, "new.txt")); err == nil {
			t.Fatalf("SECURITY: [storage-plant-write] Save wrote a file OUTSIDE BaseDir: the parent directory was swapped for a symlink after resolution and the staging write + rename followed it.")
		}
	})
}
