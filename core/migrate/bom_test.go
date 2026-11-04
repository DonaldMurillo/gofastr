package migrate

import (
	"strings"
	"testing"
)

// A migration saved by an editor that writes a byte order mark keeps its
// first directive: the mark is not part of the "-- +migrate" prefix.
func TestParseMigrationIgnoresBOM(t *testing.T) {
	src := "-- +migrate Version 7\n-- +migrate Name add_widgets\n-- +migrate Up\nCREATE TABLE widgets (id int);\n-- +migrate Down\nDROP TABLE widgets;\n"
	plain, err := parseMigration(strings.NewReader(src))
	if err != nil {
		t.Fatalf("without a BOM: %v", err)
	}
	marked, err := parseMigration(strings.NewReader("\xEF\xBB\xBF" + src))
	if err != nil {
		t.Fatalf("with a BOM: %v", err)
	}
	if marked.Version != plain.Version || marked.Name != plain.Name || marked.Up != plain.Up {
		t.Errorf("a leading BOM changed the migration: plain %+v, marked %+v", plain, marked)
	}
}
