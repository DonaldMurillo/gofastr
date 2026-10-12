package isolation

import (
	"path/filepath"
	"strings"
	"testing"
)

const remapConfig = "isolation:\n  enabled: true\n  port:\n    offset: 1000\n    range: 1\n    scan: 0\n"

// gofastr dev resolves --addr, hands the child that address as PORT plus
// the env from Runtime.Env, and the child resolves isolation again in
// ListenAddr and App.Start. Both must keep the address the parent chose:
// a dev run on --addr localhost:8791 used to land three ports away.
func TestChildBindsParentResolvedAddr(t *testing.T) {
	clearIsolationEnv(t)
	t.Setenv("PORT", "")
	dir := linkedWorktree(t)
	writeFile(t, filepath.Join(dir, "gofastr.yml"), remapConfig)
	parent, err := Resolve(dir)
	if err != nil {
		t.Fatal(err)
	}
	addr, err := parent.Addr("localhost:8791")
	if err != nil {
		t.Fatal(err)
	}
	if addr != "localhost:9791" {
		t.Fatalf("parent addr = %q, want localhost:9791", addr)
	}
	for _, kv := range parent.Env(nil) {
		k, v, _ := strings.Cut(kv, "=")
		t.Setenv(k, v)
	}
	t.Setenv("PORT", addr)
	// The child is a new process: it starts with no recorded mappings,
	// only the env its parent handed it.
	portTables.Clear()

	got, err := ListenAddr(dir, ":8080")
	if err != nil {
		t.Fatal(err)
	}
	if got != addr {
		t.Fatalf("child ListenAddr = %q, want the parent's %q", got, addr)
	}
	child, err := Resolve(dir)
	if err != nil {
		t.Fatal(err)
	}
	if started, _ := child.Addr(got); started != addr {
		t.Fatalf("child Start addr = %q, want %q", started, addr)
	}
}

// ListenAddr's doc says to pass its result to App.Start, which resolves
// isolation again. The second resolve must see the first one's mapping.
func TestListenAddrThenStartRemapsOnce(t *testing.T) {
	clearIsolationEnv(t)
	t.Setenv("PORT", "")
	dir := linkedWorktree(t)
	writeFile(t, filepath.Join(dir, "gofastr.yml"), remapConfig)
	addr, err := ListenAddr(dir, ":8080")
	if err != nil {
		t.Fatal(err)
	}
	if addr != ":9080" {
		t.Fatalf("ListenAddr = %q, want :9080", addr)
	}
	rt, err := Resolve(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := rt.Addr(addr); got != addr {
		t.Fatalf("Start addr = %q, want %q: the port was remapped twice", got, addr)
	}
}
