package main

import (
	"reflect"
	"testing"
)

func TestResolveAdapter(t *testing.T) {
	cases := []struct {
		name      string
		input     string
		wantOK    bool
		wantArgv0 string // first argv element from BuildArgs("hello"), or ""
	}{
		// Sentinels
		{"empty is none", "", false, ""},
		{"explicit none", "none", false, ""},

		// Custom command — always resolvable to a built adapter (whether
		// the binary is installed is the runtime concern).
		{"custom freeform cmd", "/bin/echo hi", true, "/bin/echo"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, ok := resolveAdapter(tc.input)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if a.BuildArgs == nil {
				t.Fatal("BuildArgs is nil for resolved adapter")
			}
			argv := a.BuildArgs("hello")
			if len(argv) == 0 {
				t.Fatal("BuildArgs returned empty argv")
			}
			if argv[0] != tc.wantArgv0 {
				t.Errorf("argv[0] = %q, want %q", argv[0], tc.wantArgv0)
			}
			// Last element must be the prompt text we passed in.
			if argv[len(argv)-1] != "hello" {
				t.Errorf("last argv = %q, want %q", argv[len(argv)-1], "hello")
			}
		})
	}
}

func TestResolveAdapterCustomBuildArgs(t *testing.T) {
	a, ok := resolveAdapter("custombin --flag1 value1 --flag2")
	if !ok {
		t.Fatal("custom freeform should resolve")
	}
	want := []string{"custombin", "--flag1", "value1", "--flag2", "the prompt"}
	got := a.BuildArgs("the prompt")
	if !reflect.DeepEqual(got, want) {
		t.Errorf("BuildArgs = %#v, want %#v", got, want)
	}
}

// When the user passes --agent as a freeform string that happens to
// equal a built-in adapter's exact spawn command, classify it as that
// named adapter — not "custom". Otherwise the gear modal can't mark a
// "current" radio (curName=="custom" matches none of the listed
// adapter rows), and the user sees an unselected list.
func TestResolveAdapterFreeformMatchesBuiltin(t *testing.T) {
	for name, want := range adapters {
		// Build the adapter's natural spawn command (argv minus the
		// trailing prompt) and feed it back through resolveAdapter as
		// a single string — the same path --agent "<freeform>" takes.
		argv := want.BuildArgs("")
		spawn := joinArgv(argv[:len(argv)-1])
		got, ok := resolveAdapter(spawn)
		if !ok {
			t.Errorf("%s: spawn cmd %q failed to resolve", name, spawn)
			continue
		}
		if got.Name != name {
			t.Errorf("%s: spawn cmd %q resolved to adapter %q, want %q (so the gear modal can mark it current)",
				name, spawn, got.Name, name)
		}
	}
}

func joinArgv(argv []string) string {
	out := ""
	for i, a := range argv {
		if i > 0 {
			out += " "
		}
		out += a
	}
	return out
}

func TestResolveAdapterUnknownName(t *testing.T) {
	// Names that aren't in the registry and aren't shell-cmd-shaped
	// (single token) should NOT resolve to "custom" — that would be
	// confusing. They go through the freeform path which then fails
	// the Detect check at runtime.
	a, ok := resolveAdapter("not-a-real-agent-name-xyz")
	if !ok {
		t.Skip("unknown bare name treated as a custom command name") // documenting current behavior
	}
	// If ok, Detect should return false (binary not on PATH).
	if a.Detect == nil {
		t.Fatal("Detect is nil")
	}
	if a.Detect() {
		t.Errorf("Detect on bogus name returned true")
	}
}
