package hook

import (
	"os"
	"strings"
	"testing"
)

func TestHookErrorDocs(t *testing.T) {
	source, err := os.ReadFile("hook.go")
	if err != nil {
		t.Fatal(err)
	}
	_, comment, _ := strings.Cut(string(source), "// HookFunc ")
	comment, _, _ = strings.Cut(comment, "type HookFunc")
	for _, want := range []string{"roll back", "read", "error"} {
		if !strings.Contains(comment, want) {
			t.Errorf("HookFunc godoc lacks %q: %s", want, comment)
		}
	}
	if strings.Contains(comment, "or log the failure") {
		t.Fatal("HookFunc godoc must not describe After errors as log-only")
	}
}
