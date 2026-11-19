// Package localstoragetest holds the shared swap-window security suite
// for the repo's LocalStorage implementations (battery/storage's local
// backend and core/upload's): the mid-resolution symlink-plant scenarios
// pinning the window between a key's containment being proven on the
// resolved chain and the operation's first filesystem syscall. Each
// implementation runs the suite through its own store constructor, its
// own afterResolve seam, and its own SECURITY wording, so breaking
// either implementation still fails its own package's test.
//
// The package has no test files of its own by design: its behavior is
// exercised through the packages that import it (see
// scripts/coverage-floors.sh on helper packages).
package localstoragetest

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/fileperm"
)

// Store is the surface the suite drives; both LocalStorage
// implementations satisfy it structurally.
type Store interface {
	Save(ctx context.Context, key string, r io.Reader) error
	Delete(ctx context.Context, key string) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	GetRange(ctx context.Context, key string) (io.ReadSeekCloser, error)
	Exists(ctx context.Context, key string) (bool, error)
}

// Suite parametrizes the shared scenarios for one implementation.
type Suite struct {
	// NewStore returns a store rooted at base.
	NewStore func(t testing.TB, base string) Store

	// SetSeam installs fn as the implementation's afterResolve seam —
	// the post-resolution, pre-syscall window every plant runs in. fn
	// may be nil, which restores the production state. The suite
	// installs one wrapper per scenario and clears the seam on cleanup;
	// tests must not run in parallel while it is installed.
	SetSeam func(fn func(string))

	// Tag prefixes the SECURITY failure tags ("storage", "upload") so a
	// failure stays attributable to the implementation that leaked.
	Tag string

	// RootNoun names the containment boundary in failure messages
	// ("BaseDir", "the storage root").
	RootNoun string
}

// RefusesMidResolvePlant runs the shared scenario table.
//
// Family: F3 path canonicalization at filesystem sinks (TOCTOU between
// the resolved containment proof and the syscalls that follow it).
// Property: proving containment on the symlink-RESOLVED chain and then
// issuing the syscall is not containment: a symlink planted into the
// root AFTER the resolution must still be refused — by the kernel, at
// the syscall, not by a check that already ran. Every surface sits
// behind the implementation's resolver, whose resolution the plant must
// not be able to invalidate between proof and use; the afterResolve
// seam drives the interleave deterministically and is nil in production.
func (s Suite) RefusesMidResolvePlant(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink planting and POSIX containment semantics are not portable on Windows")
	}

	base := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := fileperm.WriteOwnerOnly(secret, []byte("OUTSIDE-SECRET")); err != nil {
		t.Fatal(err)
	}
	// The Delete surface needs a victim file under a swappable parent.
	outsideSub := filepath.Join(outside, "sub")
	if err := os.MkdirAll(outsideSub, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := fileperm.WriteOwnerOnly(filepath.Join(outsideSub, "victim.txt"), []byte("OUTSIDE-VICTIM")); err != nil {
		t.Fatal(err)
	}

	install := func(plant func()) {
		s.SetSeam(func(string) { plant() })
	}
	t.Cleanup(func() { s.SetSeam(nil) })

	ctx := context.Background()

	// --- Read surfaces: leaf symlink planted at a key that resolved
	// clean (it did not exist), between resolution and the open. ---
	// plantLeafSymlink is the shared shape of every read-surface
	// scenario below: the outside secret lands as a leaf symlink at an
	// already-resolved key after the resolution saw a clean tree.
	plantLeafSymlink := func(t *testing.T) {
		install(func() {
			_ = os.Symlink(secret, filepath.Join(base, "victim.txt"))
		})
		t.Cleanup(func() { _ = os.Remove(filepath.Join(base, "victim.txt")) })
	}

	t.Run("Get", func(t *testing.T) {
		store := s.NewStore(t, base)
		plantLeafSymlink(t)

		rc, err := store.Get(ctx, "victim.txt")
		if err == nil {
			body, _ := io.ReadAll(rc)
			rc.Close()
			if string(body) == "OUTSIDE-SECRET" {
				t.Fatalf("SECURITY: [%s-plant-read] Get served OUTSIDE-SECRET from a symlink planted after resolution: the containment proof ran on a clean tree and the open followed a tree that was swapped underneath it.", s.Tag)
			}
		}
	})

	t.Run("GetRange", func(t *testing.T) {
		store := s.NewStore(t, base)
		plantLeafSymlink(t)

		rs, err := store.GetRange(ctx, "victim.txt")
		if err == nil {
			body, _ := io.ReadAll(rs)
			rs.Close()
			if string(body) == "OUTSIDE-SECRET" {
				t.Fatalf("SECURITY: [%s-plant-read] GetRange served OUTSIDE-SECRET from a post-resolution symlink plant.", s.Tag)
			}
		}
	})

	t.Run("Exists", func(t *testing.T) {
		store := s.NewStore(t, base)
		plantLeafSymlink(t)

		if ok, err := store.Exists(ctx, "victim.txt"); err == nil && ok {
			t.Fatalf("SECURITY: [%s-plant-exists] Exists answered true for a file OUTSIDE %s reached through a post-resolution symlink plant: an existence oracle for arbitrary paths.", s.Tag, s.RootNoun)
		}
	})

	// --- Delete surface: parent directory swapped for a symlink after
	// the key resolved through the real directory. ---
	t.Run("Delete", func(t *testing.T) {
		inRootSub := filepath.Join(base, "sub")
		if err := os.MkdirAll(inRootSub, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := fileperm.WriteOwnerOnly(filepath.Join(inRootSub, "victim.txt"), []byte("IN-ROOT")); err != nil {
			t.Fatal(err)
		}
		store := s.NewStore(t, base)
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

		_ = store.Delete(ctx, "sub/victim.txt")
		if _, err := os.Stat(filepath.Join(outsideSub, "victim.txt")); err != nil {
			t.Fatalf("SECURITY: [%s-plant-delete] Delete unlinked a file OUTSIDE %s: the parent directory was swapped for a symlink after resolution and the remove followed it.", s.Tag, s.RootNoun)
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
		store := s.NewStore(t, base)
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

		_ = store.Save(ctx, "sub2/new.txt", strings.NewReader("ESCAPED"))
		if _, err := os.Stat(filepath.Join(outsideSub2, "new.txt")); err == nil {
			t.Fatalf("SECURITY: [%s-plant-write] Save wrote a file OUTSIDE %s: the parent directory was swapped for a symlink after resolution and the staging write + rename followed it.", s.Tag, s.RootNoun)
		}
	})
}
