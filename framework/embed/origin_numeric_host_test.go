package embed

import "testing"

// A host a browser reads as IPv4 must already be a dotted quad.
func TestNormalizeOriginRefusesNumberEndingHosts(t *testing.T) {
	for origin, ok := range map[string]bool{
		"https://0x7f.0.0.1":   false,
		"https://0x7f000001":   false,
		"https://0X7F000001":   false,
		"https://127.1":        false,
		"https://example.123":  false,
		"https://example.0x":   false,
		"https://127.0.0.1":    true,
		"https://0x7f.example": true,
		"https://123.example":  true,
		"https://example.x1":   true,
		"https://[::1]":        true,
	} {
		if _, err := NormalizeOrigin(origin); (err == nil) != ok {
			t.Errorf("NormalizeOrigin(%q) err = %v, want ok=%v", origin, err, ok)
		}
	}
}
