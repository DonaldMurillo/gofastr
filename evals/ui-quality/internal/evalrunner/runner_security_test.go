package evalrunner

import (
	"path/filepath"
	"testing"
)

// Property: a caller-supplied run-id must never resolve outside the
// artifact directory it names a run under.
//
// resolveRunDirectory guards the two shapes that would turn a run-id into
// a path escape: the safeRunID charset (no separators, no leading dot,
// no escapes) and a Rel-based containment check against the resolved
// root. The lock, workspace, blind, and results trees all hang off the
// returned directory, so a traversal here would point every later
// RemoveAll and write at a chosen path outside the artifact root.
func TestRunIDCannotEscapeArtifactDir(t *testing.T) {
	root := t.TempDir()
	hostile := []string{
		"", "..", "../escape", "nested/path", "/abs",
		".hidden", "run\x00id", "run id", "run\nid",
	}
	for _, id := range hostile {
		if dir, err := resolveRunDirectory(filepath.Join(root, "runs"), id); err == nil {
			t.Errorf("SECURITY: [path-escape] resolveRunDirectory(%q) accepted, returning %s", id, dir)
		}
	}
	// Boundary control: the accepted charset still names a run directory.
	if dir, err := resolveRunDirectory(filepath.Join(root, "runs"), "20260902T120000.000000000Z"); err != nil {
		t.Errorf("resolveRunDirectory rejected a well-formed run-id: %v", err)
	} else if dir != filepath.Join(root, "runs", "20260902T120000.000000000Z") {
		t.Errorf("resolveRunDirectory returned %s, want the run directory under the artifact root", dir)
	}
}
