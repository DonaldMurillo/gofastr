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

// Two first calls for one base port can both miss the table. The one
// that records second must take the mapping already there.
func TestRecordPortKeepsFirstMapping(t *testing.T) {
	clearIsolationEnv(t)
	dir := linkedWorktree(t)
	writeFile(t, filepath.Join(dir, "gofastr.yml"), remapConfig)
	rt, err := Resolve(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := rt.recordPort(8080, 9080); got != 9080 {
		t.Fatalf("first record = %d, want 9080", got)
	}
	if got := rt.recordPort(8080, 9081); got != 9080 {
		t.Fatalf("second record = %d, want the first mapping 9080", got)
	}
}

// Resolve on a project where isolation is off maps nothing, so it must
// not add a table to the process-wide set.
func TestInactiveResolveSharesNoTable(t *testing.T) {
	clearIsolationEnv(t)
	t.Setenv(envIsolation, "off")
	dir := linkedWorktree(t)
	if _, err := Resolve(dir); err != nil {
		t.Fatal(err)
	}
	portTables.Range(func(k, _ any) bool {
		if k.(portTableKey).projectDir == dir {
			t.Fatalf("inactive Resolve(%s) left a shared table", dir)
		}
		return true
	})
}
