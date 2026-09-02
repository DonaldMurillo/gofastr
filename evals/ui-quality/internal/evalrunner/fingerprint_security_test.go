package evalrunner

import (
	"os"
	"path/filepath"
	"testing"
)

// Property: an integrity fingerprint must certify the bytes the built
// candidate will actually read, so swapping content behind a path the
// builder controls has to change the hash.
//
// All three fingerprint walkers collect files with entry.Type().IsRegular()
// on a filepath.WalkDir DirEntry, which does NOT follow symlinks: a path
// the builder planted as a symlink contributes nothing to the hash, while
// go build and the served binary read through it. The gates that lean on
// these hashes — the before/after go-test/go-build workspace gate, the
// framework-integrity check, and reusable-workspace validation — therefore
// stay green while the content the app serves comes from wherever the
// symlink points, mutable after every fingerprint was taken.
//
// Surfaces asserted: workspaceSourceFingerprint (the workspace gate and
// reuse validation) and frameworkInputFingerprint (the framework
// integrity check, which uses the same WalkDir collection for the
// subtrees under each frameworkFingerprintInputs root).
func TestFingerprintsSeeSymlinkedContent(t *testing.T) {
	payload := filepath.Join(t.TempDir(), "payload.html")
	if err := os.WriteFile(payload, []byte("benign template"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Surface 1: the workspace gate. templates/admin.html is planted as a
	// symlink before the first fingerprint, exactly as a builder would.
	ws := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, "templates"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(payload, filepath.Join(ws, "templates", "admin.html")); err != nil {
		t.Fatal(err)
	}
	before, err := workspaceSourceFingerprint(ws)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(payload, []byte("malicious template"), 0o644); err != nil {
		t.Fatal(err)
	}
	after, err := workspaceSourceFingerprint(ws)
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Errorf("SECURITY: [fingerprint-blind] workspaceSourceFingerprint is unchanged after the content behind templates/admin.html was swapped; the workspace gate certifies nothing about what the served app reads through a builder-planted symlink")
	}

	// Surface 2: the framework integrity check. The framework root is
	// built with core/pkg/a.go already a symlink — exactly as a candidate
	// with write access to its replace-directive target would leave it.
	root := t.TempDir()
	for _, rel := range frameworkFingerprintInputs {
		if rel == "go.mod" || rel == "go.sum" {
			if err := os.WriteFile(filepath.Join(root, rel), []byte("module eval.test/fp\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Join(root, rel, "pkg"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, rel, "pkg", "a.go"), []byte("package pkg"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Remove(filepath.Join(root, "core", "pkg", "a.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(payload, filepath.Join(root, "core", "pkg", "a.go")); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "gofastr-bin")
	if err := os.WriteFile(bin, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	frameworkBefore, err := frameworkInputFingerprint(root, bin)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(payload, []byte("package pkg // hijacked"), 0o644); err != nil {
		t.Fatal(err)
	}
	frameworkAfter, err := frameworkInputFingerprint(root, bin)
	if err != nil {
		t.Fatal(err)
	}
	if frameworkBefore == frameworkAfter {
		t.Errorf("SECURITY: [fingerprint-blind] frameworkInputFingerprint is unchanged after the content behind core/pkg/a.go was swapped; verifyFrameworkIntegrity passes while the candidate compiles against attacker-chosen framework code")
	}
}
