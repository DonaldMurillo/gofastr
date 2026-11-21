package idslug

import "testing"

func TestMakeKeepsEveryScript(t *testing.T) {
	for in, want := range map[string]string{
		"Hello, World!":   "hello-world",
		"  --Ünïcode--  ": "ünïcode",
		"Добрый день":     "добрый-день",
		"日本語 ２":           "日本語-２",
		"!!!":             "",
	} {
		if got := Make(in); got != want {
			t.Errorf("Make(%q) = %q, want %q", in, got, want)
		}
	}
}
