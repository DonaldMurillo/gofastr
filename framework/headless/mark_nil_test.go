package headless

import "testing"

func TestMarkFlagTakesNilAttrs(t *testing.T) {
	if got := Mark(nil, "hidden"); got["hidden"] != "" || len(got) != 1 {
		t.Fatalf("Mark(nil) = %v, want hidden set", got)
	}
	if got := Flag(nil, "open", true); len(got) != 1 {
		t.Fatalf("Flag(nil, on) = %v, want open set", got)
	}
}
