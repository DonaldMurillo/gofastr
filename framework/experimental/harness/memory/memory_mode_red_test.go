//go:build red

package memory

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// RED TEST — open finding, 2026-09-06/07 adversarial round 5, phase 2
// (family enumeration; tier T2).
// Property: memory files are owner-only on create AND overwrite — the
// fixed-name MEMORY.md index is rewritten by every Save, and the repo's
// canonical grammar for exactly that shape is documented in
// cmd/gofastr/init.go::writeEnvFile ("os.WriteFile would truncate a
// pre-existing 0644 file and write the secrets into it while it is
// still world-readable ... the mode is fixed first, on the open
// handle") and implemented in cmd/gofastr/pack.go::writeSecretFile
// (open→chmod-handle→write, pinned by TestPackOutOwnerOnlyOnOverwrite).
// Surfaces: memory.go::Store.Save :108 (os.WriteFile(e.Path, ..., 0600)
// for the per-entry file) + memory.go::writeIndexLocked :234
// (os.WriteFile(MEMORY.md, ..., 0600)) — both create-only modes.
// Finding: probe-verified — chmod MEMORY.md to 0644, Save again, and
// the index stays 0644 forever. The index lists every memory (titles,
// descriptions, hooks about the user); the per-entry .md files ride
// the same grammar through Save :108. A restored checkout or umask
// drift leaves the whole memory set group/world-readable.
// Fix direction: route both writes through the writeSecretFile
// spelling — O_CREATE|O_TRUNC 0600 open, f.Chmod(0600) on the handle,
// then write.
func TestMemoryRedOverwriteTightened(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits")
	}

	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("setup broken: New: %v", err)
	}

	if err := s.Save(Entry{
		Name:        "user_role",
		Description: "Senior Go engineer",
		Type:        TypeUser,
		Body:        "Frame explanations in Go terms.\n",
	}); err != nil {
		t.Fatalf("setup broken: first Save: %v", err)
	}

	index := filepath.Join(dir, "MEMORY.md")
	if err := os.Chmod(index, 0o644); err != nil {
		t.Fatalf("setup broken: chmod MEMORY.md: %v", err)
	}

	// Second Save rewrites the index through writeIndexLocked.
	if err := s.Save(Entry{
		Name:        "feedback_testing",
		Description: "Prefers table-driven tests",
		Type:        TypeFeedback,
		Body:        "Audit on every commit.\n",
	}); err != nil {
		t.Fatalf("setup broken: second Save: %v", err)
	}

	// Non-vacuous: the index must carry the second entry, proving this
	// Save rewrote the file.
	body, err := os.ReadFile(index)
	if err != nil {
		t.Fatalf("setup broken: read MEMORY.md: %v", err)
	}
	if !strings.Contains(string(body), "feedback_testing") {
		t.Fatalf("setup broken: MEMORY.md does not carry the second entry — overwrite did not happen: %q", body)
	}

	fi, err := os.Stat(index)
	if err != nil {
		t.Fatalf("setup broken: stat MEMORY.md: %v", err)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		t.Errorf("SECURITY: [memory-overwrite-mode] MEMORY.md is %v after overwrite — writeIndexLocked's os.WriteFile(..., 0o600) (memory.go:234, same grammar at Save :108) only applies its mode at CREATE, so a pre-existing 0644 index keeps every memory title/description group/world-readable; cmd/gofastr's writeEnvFile/writeSecretFile implement the documented chmod-on-handle fix for this exact shape (TestPackOutOwnerOnlyOnOverwrite), the harness memory store never got the cutover", fi.Mode().Perm())
	}
}
