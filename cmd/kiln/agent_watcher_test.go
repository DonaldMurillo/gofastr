package main

import (
	"strings"
	"testing"
	"time"
)

// turnSummary is the one-line system note appended at the end of every
// agent turn. Verifies the user-facing format that lands in the chat
// log so future tweaks don't silently break the closure UX.
func TestTurnSummary(t *testing.T) {
	cases := []struct {
		name      string
		tools     int
		errors    int
		dur       time.Duration
		wantParts []string
	}{
		{"happy path many tools", 5, 0, 23 * time.Second, []string{"✓", "turn complete", "5 tools", "23s"}},
		{"single tool singularizes", 1, 0, 800 * time.Millisecond, []string{"1 tool", "800ms"}},
		{"errors flip prefix and add count", 11, 2, 47 * time.Second, []string{"⚠", "11 tools", "2 errors", "47s"}},
		{"single error singularizes", 3, 1, 5 * time.Second, []string{"⚠", "1 error", "5.0s"}},
		{"chat-only reply mentions no tools", 0, 0, 1200 * time.Millisecond, []string{"no tools used", "1.2s"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := turnSummary(tc.tools, tc.errors, tc.dur)
			if !strings.HasPrefix(got, "[") || !strings.HasSuffix(got, "]") {
				t.Errorf("summary not bracketed: %q", got)
			}
			for _, p := range tc.wantParts {
				if !strings.Contains(got, p) {
					t.Errorf("summary %q missing %q", got, p)
				}
			}
		})
	}
}
