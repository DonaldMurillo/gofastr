package main

import (
	"os"
	"testing"
)

func TestSweepBasicsReservesEveryHandwrittenName(t *testing.T) {
	t.Chdir(t.TempDir())
	for name, body := range map[string]string{
		"plain.go":     "package icons\n\nconst Plain = 1\n",
		"x_windows.go": "//go:build windows\n\npackage icons\n\nfunc Windows() {}\n",
		"icons_gen.go": "package icons\n\nconst Generated = 1\n",
		"x_test.go":    "package icons\n\nconst TestOnly = 1\n",
	} {
		if err := os.WriteFile(name, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := handwrittenIdentifiers()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Plain", "Windows"} {
		if _, ok := got[want]; !ok {
			t.Errorf("%s is declared by a handwritten file and must be reserved", want)
		}
	}
	for _, skip := range []string{"Generated", "TestOnly"} {
		if _, ok := got[skip]; ok {
			t.Errorf("%s comes from the generated or a test file and must not be reserved", skip)
		}
	}
}
