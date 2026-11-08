package project

import "testing"

func TestValidWindowsFileName(t *testing.T) {
	for _, bad := range []string{
		"bad<name", "bad>name", "bad:name", `bad"name`, "bad|name",
		"bad?name", "bad*name", "bad/name", `bad\name`, "bad\nname",
		"NUL.foo", "NUL .txt", "con.txt", "COM1.exe", "LPT9.log",
		"COM¹", "COM².txt", "COM³.exe", "LPT¹", "LPT².log", "LPT³",
		"name.", "name ", ".", "..",
	} {
		if ValidWindowsFileName(bad) {
			t.Errorf("ValidWindowsFileName(%q) = true, want false", bad)
		}
	}
	for _, good := range []string{"notes", "Notes & Co", "COM10", "LPT0", "ordinary.exe", ".NET Notes", ".notes", ".exe"} {
		if !ValidWindowsFileName(good) {
			t.Errorf("ValidWindowsFileName(%q) = false, want true", good)
		}
	}
}
