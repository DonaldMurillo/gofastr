package isolation

import (
	"path/filepath"
	"testing"
)

// inactiveRuntime returns a Resolve'd runtime that is NOT isolated — the normal
// production / PaaS case.
func inactiveRuntime(t *testing.T) *Runtime {
	t.Helper()
	clearIsolationEnv(t)
	main := t.TempDir()
	writeFile(t, filepath.Join(main, ".git", "HEAD"), "ref: refs/heads/main\n")
	rt, err := Resolve(main)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if rt.Active() {
		t.Fatal("expected inactive runtime")
	}
	return rt
}

// TestAddrNormalizesBarePort pins the PaaS fix: a bare numeric PORT (as Heroku,
// Render, Railway, Cloud Run, etc. inject) must become ":<port>" so
// http.Server accepts it. Before this, Addr returned the bare value verbatim
// when isolation was inactive, so the scaffold printed "http://8088" and then
// died with "missing port in address".
func TestAddrNormalizesBarePort(t *testing.T) {
	rt := inactiveRuntime(t)
	cases := map[string]string{
		"8088":           ":8088",
		":8080":          ":8080",
		"localhost:8080": "localhost:8080",
		"":               "",
	}
	for in, want := range cases {
		got, err := rt.Addr(in)
		if err != nil {
			t.Fatalf("Addr(%q): %v", in, err)
		}
		if got != want {
			t.Errorf("Addr(%q) = %q, want %q", in, got, want)
		}
	}
}
