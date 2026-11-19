package storage

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/localstoragetest"
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
//
// The scenario flow is shared with core/upload's twin surface in
// internal/localstoragetest; this entry point binds it to this
// package's store, afterResolve seam, and SECURITY wording, so a
// regression in this implementation still fails in this package.

func TestLocalStorageRefusesMidResolvePlant(t *testing.T) {
	localstoragetest.Suite{
		NewStore: func(t testing.TB, base string) localstoragetest.Store {
			return newTestLocalStorage(t, base)
		},
		SetSeam:  func(fn func(string)) { afterResolve = fn },
		Tag:      "storage",
		RootNoun: "BaseDir",
	}.RefusesMidResolvePlant(t)
}
