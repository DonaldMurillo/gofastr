package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// Any auto-exposed entity with no per-user scoping is cross-user read/
// write/delete — every authenticated user can touch every row — not just
// the ones with PII-shaped names. The generator warns on every one of
// them so "notes"/"journal_entries"/"balances" can't slip through the
// fixed PII token list. (Auto-CRUD already requires a session by default
// — issue #65 — so this is NOT anonymous exposure; see
// TestLintPublicEntitiesFlagsPublicTrue for the entity that IS.)
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
	if msg := got[0].Message(); !strings.Contains(msg, "authenticated user") {
		t.Fatalf("finding message should spell out the cross-user exposure: %q", msg)
	}
	if msg := got[0].Message(); strings.Contains(msg, "anonymous") {
		t.Fatalf("message must not claim anonymous exposure — auto-CRUD requires a session by default (issue #65): %q", msg)
	}
}

// TestLintPublicEntitiesFlagsPublicTrue pins the actual anonymous-access
// surface post-#65: only `public: true` entities are genuinely open to
// anonymous callers. Everything lintUnscopedEntities flags above already
// requires a session — this lint is the complementary one that doesn't.
func TestLintPublicEntitiesFlagsPublicTrue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	writeTestFile(t, path, `
entities:
  - name: announcements
    public: true
    fields:
      - name: title
        type: string
  - name: journal_entries
    fields:
      - name: notes
        type: text
`)
	bp, err := loadBlueprint(path)
	if err != nil {
		t.Fatalf("loadBlueprint: %v", err)
	}
	got := lintPublicEntities(bp)
	if len(got) != 1 || got[0].Entity != "announcements" {
		t.Fatalf("lintPublicEntities = %+v, want exactly announcements", got)
	}
	msg := got[0].Message()
	if !strings.Contains(msg, "anonymous") {
		t.Fatalf("public finding message should spell out anonymous access: %q", msg)
	}
	if !strings.Contains(msg, "delete") {
		t.Fatalf("public finding message should call out write access, not just read: %q", msg)
	}
}
