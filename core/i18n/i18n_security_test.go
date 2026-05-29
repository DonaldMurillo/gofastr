package i18n

import "testing"

// TestAcceptLanguageBounded asserts that parsing of the attacker-controlled
// Accept-Language header is bounded so a single request cannot force large
// allocation + O(n log n) sort work over hundreds of thousands of segments.
func TestAcceptLanguageBounded(t *testing.T) {
	const cap = 32

	build := func(seg string, n int) string {
		b := make([]byte, 0, len(seg)*n)
		for i := 0; i < n; i++ {
			if i > 0 {
				b = append(b, ',')
			}
			b = append(b, seg...)
		}
		return string(b)
	}

	cases := []struct {
		name   string
		header string
	}{
		{"happy path", "fr-CA,fr;q=0.8,en;q=0.5"},
		{"comma flood bare tags", build("a", 200000)},
		{"comma flood with q", build("a;q=1.0", 200000)},
		{"empty segments flood", build("", 200000)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseAcceptLanguage(tc.header)
			if len(got) > cap {
				t.Fatalf("parseAcceptLanguage returned %d entries; want <= %d (unbounded parse is a DoS amplifier)", len(got), cap)
			}
		})
	}

	// The happy path must still negotiate correctly (preferred tag first).
	if got := parseAcceptLanguage("fr-CA,fr;q=0.8,en;q=0.5"); len(got) == 0 || got[0] != "fr-ca" {
		t.Fatalf("happy-path ordering broken: got %v", got)
	}
}
