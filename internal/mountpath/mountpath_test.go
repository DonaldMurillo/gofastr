package mountpath

import (
	"strings"
	"testing"
)

func TestValidateRefusesNonLiteralMounts(t *testing.T) {
	for _, tc := range []struct{ path, want string }{
		{"rel", "must be absolute"},
		{"/", "would capture the whole app"},
		{"/tail/", "must not end with a slash"},
		{"/a\x00b", "contains a control character or space"},
		{"/a b", "contains a control character or space"},
		{"/live/{room}", "must be a literal path without ServeMux metacharacters"},
		{"/a$b", "must be a literal path without ServeMux metacharacters"},
		{"/p%20th", "percent-encoding and fragments are not valid here"},
		{"/a#b", "percent-encoding and fragments are not valid here"},
		{"/a?b", "percent-encoding and fragments are not valid here"},
		{"/a\\b", "percent-encoding and fragments are not valid here"},
		{"/a//b", "contains an empty path segment"},
		{"/a/b/..", "contains a traversal segment"},
		{"/__gofastr", "reserved namespace root"},
		{"/__gofastr/sse", "collides with the reserved framework route /__gofastr/sse"},
		{"/__gofastr/runtime.js/x", "collides with the reserved framework route /__gofastr/runtime.js"},
	} {
		err := Validate("Config.Path", tc.path)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("Validate(%q) = %v, want refusal mentioning %q", tc.path, err, tc.want)
		}
	}
}

func TestValidateAcceptsLiteralMounts(t *testing.T) {
	for _, p := range []string{"/__gofastr/t", "/__gofastr/rtc", "/firstparty", "/a/b"} {
		if err := Validate("Config.Path", p); err != nil {
			t.Errorf("Validate(%q) = %v, want accepted", p, err)
		}
	}
}

// Relay validates route prefixes through ValidateSegments with the
// prefix made absolute; that spelling keeps the segment rules.
func TestValidateSegmentsRefusesBadPrefixes(t *testing.T) {
	for _, tc := range []struct{ prefix, want string }{
		{"a{b", "must be a literal path without ServeMux metacharacters"},
		{"../x", "contains a traversal segment"},
		{"a//b", "contains an empty path segment"},
		{"a%20", "percent-encoding and fragments are not valid here"},
		{"a b", "contains a control character or space"},
	} {
		err := ValidateSegments("route prefix", "/"+tc.prefix)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("ValidateSegments(%q) = %v, want refusal mentioning %q", tc.prefix, err, tc.want)
		}
	}
	if err := ValidateSegments("route prefix", "/e"); err != nil {
		t.Errorf("ValidateSegments(\"/e\") = %v, want accepted", err)
	}
}
