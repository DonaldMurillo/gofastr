package desktop

import (
	"os"
	"strings"
	"testing"
)

//gofastr:test-reads /framework/docs/content/desktop.md

func TestBootCookieDocsNameSession(t *testing.T) {
	data, err := os.ReadFile("../../framework/docs/content/desktop.md")
	if err != nil {
		t.Fatal(err)
	}
	_, authentication, found := strings.Cut(string(data), "## How the window authenticates")
	authentication, _, _ = strings.Cut(authentication, "### Local callers")
	want := "`" + sessionCookieName + "_<session value>`"
	if !found || !strings.Contains(authentication, want) {
		t.Fatalf("authentication docs do not name the per-run cookie %s", want)
	}
	if strings.Contains(authentication, "`"+sessionCookieName+"` cookie") {
		t.Fatal("authentication docs still promise the unsuffixed cookie")
	}
}
