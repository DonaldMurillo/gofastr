package appid

import (
	"strings"
	"testing"
)

func TestCheck(t *testing.T) {
	for _, id := range []string{"dev.gofastr.notes", "a.b", "com.example-app.v2"} {
		if err := Check(id); err != nil {
			t.Errorf("Check(%q) = %v", id, err)
		}
	}
	for _, id := range []string{"", "nodot", ".", "..", "...", ".-.", "a/b.c", "a b.c", "a_b.c", "a~b.c", strings.Repeat("a", 250) + ".com"} {
		if Check(id) == nil {
			t.Errorf("Check(%q) accepted", id)
		}
	}
}

func TestCheckName(t *testing.T) {
	for _, name := range []string{"Notes", `Notes 100% "$HOME"`, strings.Repeat("n", 64)} {
		if err := CheckName(name); err != nil {
			t.Errorf("CheckName(%q) = %v", name, err)
		}
	}
	for _, name := range []string{"", strings.Repeat("n", 65), "a/b", `a\b`, "tab\there", "new\nline", "del\x7f"} {
		if CheckName(name) == nil {
			t.Errorf("CheckName(%q) accepted", name)
		}
	}
}
