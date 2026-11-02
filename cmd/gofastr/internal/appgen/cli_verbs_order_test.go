package appgen

import "testing"

// --verbs naming two unknown entities is refused naming the same one on
// every run: buildCLISpec ranged the per-entity map, so the refusal named
// whichever entity the iteration reached first.
func TestCLIVerbsRefusalIsDeterministic(t *testing.T) {
	opts := defaultCLIOptions()
	opts.verbs = "ghost1=list;ghost2=list;ghost3=list;ghost4=list"
	_, first := buildCLISpec(camelEntityDecls(), opts, "example.com/app/entities/client")
	if first == nil {
		t.Fatal("unknown entities accepted")
	}
	for range 64 {
		if _, err := buildCLISpec(camelEntityDecls(), opts, "example.com/app/entities/client"); err == nil || err.Error() != first.Error() {
			t.Fatalf("refusal changed between runs: %q then %v", first, err)
		}
	}
}
