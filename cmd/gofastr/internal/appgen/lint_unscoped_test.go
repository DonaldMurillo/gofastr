package appgen

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// The generator warns about unscoped reads even for fields without PII names.
func TestLintUnscopedFlagsNonPIIEntities(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	clitest.WriteTestFile(t, path, `
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
  - name: read_gated
    access:
      read: gated:read
    fields:
      - name: body
        type: text
  - name: hidden
    crud: false
    fields:
      - name: body
        type: text
`)
	bp, err := appspec.LoadAppSpec(path)
	if err != nil {
		t.Fatalf("loadAppSpec: %v", err)
	}
	got := lintUnscopedEntities(bp)
	if len(got) != 2 || got[0].Entity != "journal_entries" || got[1].Entity != "gated" {
		t.Fatalf("lintUnscopedEntities = %+v, want journal_entries and gated", got)
	}
	if msg := got[0].Message(); !strings.Contains(msg, "authenticated user") {
		t.Fatalf("finding message should spell out the cross-user exposure: %q", msg)
	}
	if msg := got[0].Message(); strings.Contains(msg, "anonymous") {
		t.Fatalf("message must not claim anonymous exposure — auto-CRUD requires a session by default (issue #65): %q", msg)
	}
	if msg := got[1].Message(); !strings.Contains(msg, "anonymous callers can read") || strings.Contains(msg, "session is already required") {
		t.Fatalf("a create-only permission leaves anonymous reads open: %q", msg)
	}
}

// Public entities get a separate warning about anonymous reads and writes.
func TestLintPublicEntitiesFlagsPublicTrue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	clitest.WriteTestFile(t, path, `
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
	bp, err := appspec.LoadAppSpec(path)
	if err != nil {
		t.Fatalf("loadAppSpec: %v", err)
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
