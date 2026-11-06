package framework

import (
	"os"
	"strings"
	"testing"
)

//gofastr:test-reads /framework/docs/content/hooks-and-transactions.md
func TestTypedAfterHookCommitDocs(t *testing.T) {
	source, err := os.ReadFile("typed_hooks.go")
	if err != nil {
		t.Fatal(err)
	}
	_, comment, _ := strings.Cut(string(source), "// OnAfterCreate ")
	comment, _, _ = strings.Cut(comment, "func OnAfterCreate")
	if strings.Contains(comment, "already committed") || !strings.Contains(comment, "before commit") || !strings.Contains(comment, "rolls back") {
		t.Errorf("typed AfterCreate must document transactional timing: %s", comment)
	}
	document, err := os.ReadFile("docs/content/hooks-and-transactions.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, stale := range []string{"Create/Update has already committed it", "Create/Update have already committed the row"} {
		if strings.Contains(string(document), stale) {
			t.Errorf("hook guide still claims %q", stale)
		}
	}
}
