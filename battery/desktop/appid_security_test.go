package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// PROPERTY
//
//	An identifier that is interpolated into a filesystem path rejects
//	the "." and ".." segments, and a value read back off disk that is
//	used as a security principal is re-validated against the grammar
//	that minted it.
//
// SURFACE 1, the app id (D-04)
//
//	reAppID is `^[A-Za-z0-9.-]+$` plus "must contain a dot". ".." matches
//	both, and DataDir does filepath.Join(base, id), so DataDir("..")
//	resolves to the PARENT of the OS user config dir and MkdirAll's it.
//	Everything downstream then writes there: app.db, the 0600 session
//	secret, and the identity file that IS the local admin's owner id.
//	`gofastr desktop build --id ..` bakes that into CFBundleIdentifier,
//	so a built bundle does it on every launch.
//
//	The grammar excludes "/", so multi-segment traversal is impossible;
//	the escape is exactly one level and only for the "." / ".." spellings.
//	Developer/CLI input today, but DataDir is EXPORTED, the grammar is
//	duplicated in cmd/gofastr/desktop.go (validateDesktopID) where --id
//	comes from argv, and a validator outlives the caller that controls
//	its input.
//
// SURFACE 2, the identity file (D-13)
//
//	loadOrMintLocalUser returns &localUser{id: string(b)} with the file's
//	bytes verbatim. A truncated, newline-terminated or oversized file
//	becomes the owner id every owner-scoped query compares against and
//	the string GetEmail() interpolates. An id differing by one trailing
//	newline from the one that wrote the rows silently hides every note;
//	that corruption needs no attacker at all.

// TestAppIDRejectsDotSegments pins surface 1.
func TestAppIDRejectsDotSegments(t *testing.T) {
	// One happy path, then the distinct escape spellings.
	if err := validateAppID("dev.gofastr.notes"); err != nil {
		t.Fatalf("a normal reverse-DNS id was refused: %v", err)
	}
	for _, id := range []string{"..", ".", "...", "..-..", "a/../b", ".."} {
		if err := validateAppID(id); err == nil {
			t.Errorf("validateAppID(%q) accepted it; DataDir would then join it into the OS user config dir "+
				"and write app.db, the session secret and the local owner id outside the app's own directory", id)
		}
	}
}

// TestDataDirStaysUnderItsBase proves the consequence, not just the
// grammar: whatever the validator allows, the resolved directory must
// stay inside the base.
func TestDataDirStaysUnderItsBase(t *testing.T) {
	base := t.TempDir()
	t.Setenv(dataDirEnv, base)

	resolvedBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"..", ".", "..."} {
		dir, err := DataDir(id)
		if err != nil {
			continue // refused: that is the secure answer
		}
		resolved, rerr := filepath.EvalSymlinks(dir)
		if rerr != nil {
			resolved = dir
		}
		if resolved == resolvedBase || !strings.HasPrefix(resolved+string(os.PathSeparator), resolvedBase+string(os.PathSeparator)) {
			t.Errorf("DataDir(%q) = %q, outside the base %q", id, resolved, resolvedBase)
		}
	}
}

// TestIdentityFileIsRevalidated pins surface 2.
func TestIdentityFileIsRevalidated(t *testing.T) {
	for _, content := range []string{
		"0123456789abcdef0123456789abcdef\n", // trailing newline
		"  0123456789abcdef0123456789abcdef", // leading space
		"short",                              // truncated
		"not-hex-not-hex-not-hex-not-hexx",   // right length, wrong alphabet
		strings.Repeat("a", 4096),            // oversized
	} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, identityFileName), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		u, err := loadOrMintLocalUser(dir)
		if err != nil {
			continue // refused or repaired: acceptable
		}
		if len(u.GetID()) != 32 || strings.Trim(u.GetID(), "0123456789abcdef") != "" {
			t.Errorf("identity file %q became owner id %q; a value used as a security principal must be "+
				"re-validated against the 32-hex grammar that minted it", content, u.GetID())
		}
	}
}

// TestIdentityMintingStillRoundTrips is the anti-vacuity half.
func TestIdentityMintingStillRoundTrips(t *testing.T) {
	dir := t.TempDir()
	first, err := loadOrMintLocalUser(dir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := loadOrMintLocalUser(dir)
	if err != nil {
		t.Fatal(err)
	}
	if first.GetID() != second.GetID() || len(first.GetID()) != 32 {
		t.Fatalf("minted id %q then read back %q; the identity must be stable and 32 hex chars",
			first.GetID(), second.GetID())
	}
}
