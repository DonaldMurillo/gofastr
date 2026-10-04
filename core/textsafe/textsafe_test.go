package textsafe

import "testing"

func TestStripInvisibleRemovesBidiAndZeroWidthOnly(t *testing.T) {
	cases := map[string]string{
		"":                 "",
		"plain":            "plain",
		"a\u200bb":         "ab",
		"\u202eevil\u202c": "evil",
		"\ufeffbom":        "bom",
		// C0/C1 controls stay: callers scrub those separately.
		"tab\tkept":     "tab\tkept",
		"nel\u0085kept": "nel\u0085kept",
		"日本\u200d語":     "日本語",
	}
	for in, want := range cases {
		if got := StripInvisible(in); got != want {
			t.Errorf("StripInvisible(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStripInvisibleCleanFastPath(t *testing.T) {
	in := "nothing to strip"
	if got := StripInvisible(in); got != in {
		t.Fatalf("clean input changed: %q", got)
	}
}

func TestContainsInvisible(t *testing.T) {
	yes := []string{"\u200b", "a\u202eb", "\ufeff", "\u2066x\u2069"}
	no := []string{"", "plain", "\t\n", "\u0085", "日本語", "é"}
	for _, s := range yes {
		if !ContainsInvisible(s) {
			t.Errorf("ContainsInvisible(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if ContainsInvisible(s) {
			t.Errorf("ContainsInvisible(%q) = true, want false", s)
		}
	}
}

func TestContainsUnsafe(t *testing.T) {
	yes := []string{"\x00", "\t", "\x7f", "\u0085", "\u009b", "\u200b", "a\u202eb"}
	no := []string{"", "plain text", "~", "日本語", "héllo", "emoji 🙂"}
	for _, s := range yes {
		if !ContainsUnsafe(s) {
			t.Errorf("ContainsUnsafe(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if ContainsUnsafe(s) {
			t.Errorf("ContainsUnsafe(%q) = true, want false", s)
		}
	}
}

func TestTruncateLeavesShortStringsAlone(t *testing.T) {
	for _, s := range []string{"", "short", "exactly ten"} {
		if got := Truncate(s, len(s)); got != s {
			t.Errorf("Truncate(%q, len) = %q", s, got)
		}
		if got := Truncate(s, len(s)+1); got != s {
			t.Errorf("Truncate(%q, len+1) = %q", s, got)
		}
	}
}

func TestTruncateAppendsMarkerWithinCap(t *testing.T) {
	const marker = " … (truncated)"
	in := "0123456789abcdefghijklmnopqrstuvwxyz0123456789"
	max := 30
	got := Truncate(in, max)
	if len(got) != max {
		t.Fatalf("len(Truncate) = %d, want %d: %q", len(got), max, got)
	}
	if got[:max-len(marker)] != in[:max-len(marker)] {
		t.Fatalf("prefix changed: %q", got)
	}
	if got[max-len(marker):] != marker {
		t.Fatalf("marker missing: %q", got)
	}
}

func TestTruncateHardCutWhenMarkerDoesNotFit(t *testing.T) {
	const marker = " … (truncated)"
	in := "0123456789abcdefghijklmnopqrstuvwxyz"
	for _, max := range []int{0, 1, 5, len(marker)} {
		got := Truncate(in, max)
		if got != in[:max] {
			t.Errorf("Truncate(_, %d) = %q, want %q", max, got, in[:max])
		}
	}
}
