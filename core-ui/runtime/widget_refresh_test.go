package runtime

import (
	"os"
	"strings"
	"testing"
)

func TestWidgetSSERefreshUsesSPANavigation(t *testing.T) {
	src, err := os.ReadFile("src/widgets.js")
	if err != nil {
		t.Fatal(err)
	}
	text := string(src)
	if strings.Contains(text, "location.reload()") {
		t.Fatal("widget SSE refresh must not hard-reload the page")
	}
	if !strings.Contains(text, "NS.navigate(path, { force: true })") {
		t.Fatal("widget SSE refresh must bypass the SPA screen cache")
	}
}
