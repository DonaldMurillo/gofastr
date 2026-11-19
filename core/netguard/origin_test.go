package netguard

import (
	"net/http/httptest"
	"testing"
)

func TestOriginAllowed(t *testing.T) {
	cases := []struct {
		origin  string
		allowed []string
		want    bool
	}{
		{"", nil, true},
		{"https://App.example", nil, true},
		{"https://evil.example", nil, false},
		{"null", nil, false},
		{"https://evil.example", []string{"HTTPS://EVIL.example"}, true},
		{"https://evil.example", []string{"https://other.example"}, false},
	}
	for _, c := range cases {
		r := httptest.NewRequest("POST", "http://app.example/rpc", nil)
		if c.origin != "" {
			r.Header.Set("Origin", c.origin)
		}
		if got := OriginAllowed(r, c.allowed); got != c.want {
			t.Errorf("OriginAllowed(%q, %v) = %v, want %v", c.origin, c.allowed, got, c.want)
		}
	}
}
