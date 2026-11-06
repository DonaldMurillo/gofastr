package docs

import (
	"strings"
	"testing"
)

func TestHealthDocsNameRootMiddleware(t *testing.T) {
	document, err := Get("health-checks")
	if err != nil {
		t.Fatal(err)
	}
	text := string(document)
	if strings.Contains(text, "sit ahead of") || strings.Contains(text, "mount\n  these endpoints before") || !strings.Contains(text, "root middleware chain") || !strings.Contains(text, "route groups") {
		t.Fatal("health probes run through root middleware; authentication must exempt them or use route groups")
	}
}
