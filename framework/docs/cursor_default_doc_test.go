package docs

import (
	"strings"
	"testing"
)

func TestCursorDocumentsSharedListDefault(t *testing.T) {
	document, err := Get("cursor-pagination")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(document), "DefaultPageSize = 20") || strings.Contains(string(document), "DefaultPageSize = 25") {
		t.Fatal("cursor default must match the shared entity list page size of 20")
	}
}
