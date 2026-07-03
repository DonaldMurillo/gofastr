package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// Any auto-exposed entity with no scoping is anonymous world read/write/
// delete — not just the ones with PII-shaped names. The generator warns on
// every one of them so "notes"/"journal_entries"/"balances" can't slip
// through the fixed PII token list.
func TestLintUnscopedFlagsNonPIIEntities(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	writeTestFile(t, path, `
entities:
  - name: journal_entries
    fields:
      - name: user_id
        type: string
      - name: notes
        type: text
  - name: scoped_notes
    owner_field: user_id
    fields:
      - name: user_id
        type: string
      - name: notes
        type: text
  - name: gated
    access:
      create: gated:write
    fields:
      - name: body
        type: text
  - name: hidden
    crud: false
    fields:
      - name: body
        type: text
`)
	bp, err := loadBlueprint(path)
	if err != nil {
		t.Fatalf("loadBlueprint: %v", err)
	}
	got := lintUnscopedEntities(bp)
	if len(got) != 1 || got[0].Entity != "journal_entries" {
		t.Fatalf("lintUnscopedEntities = %+v, want exactly journal_entries", got)
	}
	if msg := got[0].Message(); !strings.Contains(msg, "anonymous") {
		t.Fatalf("finding message should spell out the anonymous exposure: %q", msg)
	}
}
