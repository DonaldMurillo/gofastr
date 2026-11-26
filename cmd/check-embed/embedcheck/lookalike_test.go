package embedcheck

import "testing"

func TestLookalikesAreNotTheFramework(t *testing.T) {
	findings, notes := loadAll(t, "lookalike")
	if len(findings) != 0 {
		t.Fatalf("a same-named Surface, NewScreen, On or WithClientJS was read as the framework's: %+v", findings)
	}
	if len(notes) != 1 || notes[0].Surface != "fake-screen" || notes[0].Blocking {
		t.Fatalf("want one advisory note for the fake NewScreen, got %+v", notes)
	}
}
