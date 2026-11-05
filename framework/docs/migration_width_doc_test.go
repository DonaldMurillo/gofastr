package docs

import (
	"strings"
	"testing"
)

func TestMigrationsDocumentVarcharWidths(t *testing.T) {
	document, err := Get("migrations")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(document), "but NOT\nsize/precision changes") || !strings.Contains(string(document), "compares character widths on Postgres") {
		t.Fatal("migrations must describe Postgres character-width detection separately from normalized decimal precision")
	}
}
