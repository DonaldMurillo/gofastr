package lifecycle_test

import (
	"os"
	"strings"
	"testing"
)

//gofastr:test-reads lifecycle.go
func TestShutdownDocNamesDrainOrder(t *testing.T) {
	source, err := os.ReadFile("lifecycle.go")
	if err != nil {
		t.Fatal(err)
	}
	_, description, _ := strings.Cut(string(source), "// Shutdown executes")
	description, _, _ = strings.Cut(description, "func (lc *Lifecycle) Shutdown")
	if strings.Contains(description, "concurrently") || !strings.Contains(description, "registration order") || !strings.Contains(description, "one shared timeout") {
		t.Fatal("Shutdown must document sequential drainers in registration order under one shared timeout")
	}
}
