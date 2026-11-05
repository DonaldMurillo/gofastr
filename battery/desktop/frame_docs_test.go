package desktop

import (
	"os"
	"strings"
	"testing"
)

func TestFrameCallbackDocsIncludeSecondary(t *testing.T) {
	source, err := os.ReadFile("shell.go")
	if err != nil {
		t.Fatal(err)
	}
	_, comment, found := strings.Cut(string(source), "// OnWindowFrame ")
	comment, _, _ = strings.Cut(comment, "OnWindowFrame func")
	if !found || strings.Contains(comment, "never fires") || !strings.Contains(comment, "secondary") {
		t.Fatalf("frame callback must include secondary windows: %s", comment)
	}
}
