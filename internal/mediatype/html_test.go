package mediatype

import "testing"

func TestIsHTML(t *testing.T) {
	for _, tt := range []struct {
		name, contentType string
		want              bool
	}{
		{"plain", "text/html", true},
		{"mixed_case", "tExT/HtMl", true},
		{"parameters", "Text/HTML; charset=utf-8", true},
		{"missing_semicolon", "text/html charset=utf-8", true},
		{"tab_parameters", "text/html\tcharset=utf-8", true},
		{"whitespace", " text/html ; charset=utf-8", true},
		{"trailing_whitespace", "text/html \t", true},
		{"malformed_parameter", "text/html; charset", true},
		{"mixed_case_malformed_parameter", "Text/HTML; charset", true},
		{"suffix", "text/htmlish", false},
		{"empty", "", false},
		{"blank", " \t", false},
		{"json", "application/json", false},
		{"xhtml", "application/xhtml+xml", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsHTML(tt.contentType); got != tt.want {
				t.Fatalf("IsHTML(%q) = %v, want %v", tt.contentType, got, tt.want)
			}
		})
	}
}
