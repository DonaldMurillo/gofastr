package netguard

import "testing"

func TestIsLoopbackAuthority(t *testing.T) {
	cases := []struct {
		authority string
		want      bool
	}{
		{"localhost", true},
		{"LOCALHOST:8080", true},
		{"127.0.0.1", true},
		{"127.0.0.1:8765", true},
		{"127.1.2.3:80", true},
		{"[::1]:443", true},
		{"::1", true},
		{"10.0.0.1:80", false},
		{"192.168.1.1", false},
		{"example.com:443", false},
		{"localhost.evil.example", false},
		{"", false},
		{"[fe80::1]:80", false},
	}
	for _, c := range cases {
		if got := IsLoopbackAuthority(c.authority); got != c.want {
			t.Errorf("IsLoopbackAuthority(%q) = %v, want %v", c.authority, got, c.want)
		}
	}
}
