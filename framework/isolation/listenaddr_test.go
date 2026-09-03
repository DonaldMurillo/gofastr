package isolation

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestListenAddrHonoursPortShapes(t *testing.T) {
	t.Setenv("GOFASTR_ISOLATION", "off")
	cases := []struct{ port, fallback, want string }{
		{"", ":3090", ":3090"},                        // unset: fallback as given
		{"8088", ":3090", ":8088"},                    // PaaS bare port
		{"localhost:8123", ":3090", "localhost:8123"}, // gofastr dev host:port
		{"", "localhost:8080", "localhost:8080"},
	}
	for _, c := range cases {
		t.Setenv("PORT", c.port)
		got, err := ListenAddr(".", c.fallback)
		if err != nil {
			t.Fatalf("PORT=%q: %v", c.port, err)
		}
		if got != c.want {
			t.Errorf("PORT=%q fallback=%q: got %q, want %q", c.port, c.fallback, got, c.want)
		}
	}
}

func TestListenAddrRemapsInALinkedWorktree(t *testing.T) {
	t.Setenv("GOFASTR_ISOLATION", "")
	t.Setenv("GOFASTR_ISOLATION_REWRITE", "")
	t.Setenv("PORT", "")
	dir := t.TempDir()
	// A .git FILE pointing at a worktrees/ gitdir is what marks a linked
	// worktree, the default trigger for isolation.
	writeFile(t, filepath.Join(dir, ".git"), "gitdir: "+filepath.Join(t.TempDir(), ".git", "worktrees", "feature")+"\n")
	got, err := ListenAddr(dir, ":8080")
	if err != nil {
		t.Fatal(err)
	}
	if got == ":8080" {
		t.Fatal("linked worktree left :8080 unremapped; ListenAddr is not applying Runtime.Addr")
	}
	if _, port, _, err := splitAddr(got); err != nil || port <= 0 {
		t.Fatalf("remapped address %q is not a valid listen address: %v", got, err)
	}
}

func TestListenAddrReturnsResolveErrors(t *testing.T) {
	t.Setenv("GOFASTR_ISOLATION", "on")
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "gofastr.isolation.yml"), "isolation:\n  mode: bogus\n")
	_, err := ListenAddr(dir, ":8080")
	if err == nil {
		t.Fatal("a config with an unsupported mode resolved; the Resolve error is being swallowed")
	}
	if !strings.Contains(err.Error(), "mode") {
		t.Fatalf("error is not the unsupported-mode refusal: %v", err)
	}
}
