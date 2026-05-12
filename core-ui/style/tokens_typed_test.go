package style

import (
	"testing"
	"time"
)

func TestTypedTokensCSSShape(t *testing.T) {
	cases := []struct {
		got, want string
	}{
		{Color{Name: "primary", Value: "#4F46E5"}.CSS(), "var(--color-primary)"},
		{Spacing{Name: "md", Value: 8}.CSS(), "var(--spacing-md)"},
		{Radius{Name: "lg", Value: 12}.CSS(), "var(--radii-lg)"},
		{Font{Name: "body", Value: "Inter"}.CSS(), "var(--font-body)"},
		{Breakpoint{Name: "md", Value: 768}.CSS(), "var(--breakpoint-md)"},
		{Shadow{Name: "md", Value: "0 4px 6px rgba(0,0,0,0.1)"}.CSS(), "var(--shadow-md)"},
		{ZIndexValue{Name: "modal", Value: 100}.CSS(), "var(--z-modal)"},
		{Duration{Name: "fast", Value: 150 * time.Millisecond}.CSS(), "var(--duration-fast)"},
		{FontSize{Name: "base", Value: "1rem"}.CSS(), "var(--text-base)"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("CSS() = %q, want %q", c.got, c.want)
		}
	}
}

func TestDurationFormattedValue(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{150 * time.Millisecond, "150ms"},
		{1 * time.Second, "1000ms"},
		{500 * time.Microsecond, "500us"},
	}
	for _, c := range cases {
		got := Duration{Name: "x", Value: c.d}.FormattedValue()
		if got != c.want {
			t.Errorf("FormattedValue(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

// TestTokensAreStringers ensures every typed token implements Stringer
// (via .String()) so they substitute cleanly into fmt.Sprintf / Println.
func TestTokensAreStringers(t *testing.T) {
	checks := []interface {
		String() string
	}{
		Color{Name: "x"},
		Spacing{Name: "x"},
		Radius{Name: "x"},
		Font{Name: "x"},
		Breakpoint{Name: "x"},
		Shadow{Name: "x"},
		ZIndexValue{Name: "x"},
		Duration{Name: "x"},
		FontSize{Name: "x"},
	}
	for _, c := range checks {
		if c.String() == "" {
			t.Errorf("%T.String() empty", c)
		}
	}
}
