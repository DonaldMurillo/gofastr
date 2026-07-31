package ulid

import (
	"strings"
	"testing"
	"time"
)

func TestNewIsValid(t *testing.T) {
	for i := 0; i < 100; i++ {
		u, err := New()
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		s := u.String()
		if len(s) != Length {
			t.Fatalf("length = %d, want %d (%q)", len(s), Length, s)
		}
		if !IsValid(s) {
			t.Fatalf("IsValid(%q) = false", s)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	for i := 0; i < 100; i++ {
		u, _ := New()
		s := u.String()
		parsed, err := Parse(s)
		if err != nil {
			t.Fatalf("Parse(%q): %v", s, err)
		}
		if parsed != u {
			t.Fatalf("round-trip mismatch:\n got  %v\n want %v", parsed, u)
		}
	}
}

func TestTimeRecoverable(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	u, err := NewAt(now)
	if err != nil {
		t.Fatal(err)
	}
	got := u.Time()
	if !got.Equal(now) {
		t.Fatalf("Time = %v, want %v", got, now)
	}
}

func TestSortable(t *testing.T) {
	// Two ULIDs at different times should sort in time order.
	earlier, _ := NewAt(time.UnixMilli(1000))
	later, _ := NewAt(time.UnixMilli(2000))
	if earlier.String() >= later.String() {
		t.Fatalf("expected earlier < later lexicographically:\n earlier %s\n later   %s",
			earlier.String(), later.String())
	}
}

func TestPrefixed(t *testing.T) {
	s, err := NewPrefixed("sess")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(s, "sess_") {
		t.Fatalf("missing prefix: %q", s)
	}
	prefix, _, err := SplitPrefixed(s)
	if err != nil {
		t.Fatalf("SplitPrefixed: %v", err)
	}
	if prefix != "sess" {
		t.Fatalf("prefix = %q, want sess", prefix)
	}
}

func TestSplitPrefixedRejectsMalformed(t *testing.T) {
	cases := []string{
		"",
		"sess",
		"sess_",
		"_01H123",
		"sess_too-short",
	}
	for _, s := range cases {
		if _, _, err := SplitPrefixed(s); err == nil {
			t.Errorf("SplitPrefixed(%q) = nil, want error", s)
		}
	}
}

func TestIsValidRejects(t *testing.T) {
	cases := []string{
		"",
		"short",
		strings.Repeat("Z", Length-1),
		// First char > '7' → overflow.
		"Z" + strings.Repeat("0", Length-1),
		// Crockford excludes I/L/O/U.
		"I" + strings.Repeat("0", Length-1),
	}
	for _, s := range cases {
		if IsValid(s) {
			t.Errorf("IsValid(%q) = true, want false", s)
		}
	}
}

func TestFromSeedDeterministic(t *testing.T) {
	seed := []byte("0123456789abcdef")
	a := FromSeed(seed)
	b := FromSeed(seed)
	if a != b {
		t.Fatalf("FromSeed not deterministic")
	}
}
