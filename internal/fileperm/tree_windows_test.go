package fileperm

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestRestrictDirectoryTreeStopsAtRootWithDifferentCase(t *testing.T) {
	root := filepath.Join(t.TempDir(), "ROOT")
	leaf := filepath.Join(filepath.Dir(root), "root", "tenant", "app")
	var visited []string
	err := restrictDirectoryTree(leaf, root, func(path string, directory bool) error {
		if !directory {
			t.Error("tree restriction requested a file ACL")
		}
		visited = append(visited, path)
		return nil
	})
	if err != nil {
		t.Fatalf("restrict tree: %v (visited %v)", err, visited)
	}
	if len(visited) != 3 || !strings.EqualFold(visited[len(visited)-1], root) {
		t.Fatalf("ACL targets = %v; want leaf, tenant, and root only", visited)
	}
}

func TestRestrictDirectoryTreeRejectsOutsideBeforeChangingACL(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	called := false
	err := restrictDirectoryTree(filepath.Join(root, "..", "outside"), root, func(string, bool) error {
		called = true
		return nil
	})
	if err == nil || called {
		t.Fatalf("outside root: error = %v, ACL changed = %v", err, called)
	}
}
