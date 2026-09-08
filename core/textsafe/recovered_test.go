package textsafe

import (
	"strings"
	"testing"
)

func TestRecoveredStripsControlBytes(t *testing.T) {
	got := Recovered("bad \x1b]0;pwned\x07 ‮  end")
	for _, r := range got {
		if IsUnsafe(r) {
			t.Fatalf("Recovered left %U in %q", r, got)
		}
	}
	if !strings.HasPrefix(got, "bad ") || !strings.HasSuffix(got, " end") {
		t.Fatalf("Recovered mangled the visible text: %q", got)
	}
}

func TestRecoveredTruncates(t *testing.T) {
	got := Recovered(strings.Repeat("é", maxRecoveredLen))
	if len(got) > maxRecoveredLen+len("…(truncated)") {
		t.Fatalf("Recovered did not truncate: %d bytes", len(got))
	}
	if !strings.HasSuffix(got, "…(truncated)") {
		t.Fatalf("Recovered lost the truncation marker: %q", got[len(got)-20:])
	}
	if !strings.HasPrefix(got, "éé") {
		t.Fatalf("Recovered cut inside a rune: %q", got[:8])
	}
}

func TestRecoveredRendersErrors(t *testing.T) {
	if got := Recovered(errString("boom")); got != "boom" {
		t.Fatalf("Recovered(error) = %q, want boom", got)
	}
}

type errString string

func (e errString) Error() string { return string(e) }
