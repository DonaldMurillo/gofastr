package mediatype

import (
	"slices"
	"testing"
)

func TestAcceptRanges(t *testing.T) {
	for _, tc := range []struct {
		name, field string
		want        []string
	}{
		{"quoted comma", `text/plain;p="a,b",text/html`, []string{`text/plain;p="a,b"`, "text/html"}},
		{"escaped quote", `text/plain;p="a\"b,c",text/html`, []string{`text/plain;p="a\"b,c"`, "text/html"}},
		{"escaped backslash", `text/plain;p="a\\",text/html`, []string{`text/plain;p="a\\"`, "text/html"}},
		{"backslash outside quotes", `text/plain\,text/html`, []string{`text/plain\`, "text/html"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := slices.Collect(AcceptRanges(tc.field)); !slices.Equal(got, tc.want) {
				t.Errorf("AcceptRanges(%q) = %q, want %q", tc.field, got, tc.want)
			}
		})
	}
}

func TestAcceptsAny(t *testing.T) {
	for _, tc := range []struct {
		name, field string
		want        bool
	}{
		{"quoted comma", `text/plain;p="a, Text/HTML, b"`, true},
		{"unclosed quote", `text/plain;p="a, Text/HTML;q=0`, true},
		{"escaped quote", `text/plain;p="a\", Text/HTML;q=0",application/json`, true},
		{"no matching range", `text/plain;p="Text/HTML",application/json`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := AcceptsAny(tc.field, "text/html"); got != tc.want {
				t.Errorf("AcceptsAny(%q, text/html) = %t, want %t", tc.field, got, tc.want)
			}
		})
	}
}
