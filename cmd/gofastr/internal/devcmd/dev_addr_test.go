package devcmd

import "testing"

// An address no listener can take used to start the whole loop anyway:
// `-p abc`, `-p 99999` and `-p ""` printed "Server at http://localhost:…",
// warned that the hold could not bind, built the app, and kept watching
// with nothing serving (the empty port bound a random one the banner
// never named). Each is refused before anything runs.
func TestParseDevFlagsRefusesBadAddr(t *testing.T) {
	for _, args := range [][]string{
		{"-p", "abc"},
		{"-p", "99999"},
		{"-p=-1"},
		{"-p="},
		{"--addr=localhost:"},
		{"--addr", "localhost:http"},
		{"--addr", "localhost"},
		{"--addr", "70000"},
		{"--addr="},
	} {
		if addr, _, _, _, err := parseDevFlags(args); err == nil {
			t.Errorf("parseDevFlags(%q) accepted addr %q", args, addr)
		}
	}
	for _, args := range [][]string{
		{"-p", "0"},
		{"-p", "65535"},
		{"--addr", "9000"},
		{"--addr", ":9000"},
		{"--addr", "[::1]:9000"},
		{"--addr", "127.0.0.1:9000"},
	} {
		if _, _, _, _, err := parseDevFlags(args); err != nil {
			t.Errorf("parseDevFlags(%q): %v", args, err)
		}
	}
}
