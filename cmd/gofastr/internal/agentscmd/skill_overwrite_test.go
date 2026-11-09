package agentscmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// #669: `gofastr agents skill` overwrote a hand-edited SKILL.md without
// asking, while the .agents/ detail files already refused. The generated
// SKILL.md carries its header in the frontmatter, and an existing file
// without it is refused unless --force.

func TestWriteHostSkillRefusesHandWritten(t *testing.T) {
	dir := t.TempDir()
	skill := filepath.Join(dir, hostSkillDir, "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skill), 0o755); err != nil {
		t.Fatal(err)
	}
	handWritten := "---\nname: gofastr-host\ndescription: my own words\n---\n\nMy project's rules.\n"
	if err := os.WriteFile(skill, []byte(handWritten), 0o644); err != nil {
		t.Fatal(err)
	}

	err := WriteHostSkill(dir, false)
	if err == nil {
		t.Fatal("a hand-written SKILL.md was overwritten without --force")
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Fatalf("refusal must name --force: %v", err)
	}
	got, rerr := os.ReadFile(skill)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if string(got) != handWritten {
		t.Error("refused run still wrote the file")
	}

	// --force replaces it.
	if err := WriteHostSkill(dir, true); err != nil {
		t.Fatalf("--force write: %v", err)
	}
	got, rerr = os.ReadFile(skill)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !strings.HasPrefix(string(got), "---\n"+skillGenHeader) {
		t.Errorf("--force wrote a file without the generated header")
	}
}

// A previous generated copy (header present) is refreshed without any
// flag, and a missing file is written without one.
func TestWriteHostSkillRefreshesGeneratedCopy(t *testing.T) {
	dir := t.TempDir()

	// Missing: plain write.
	if err := WriteHostSkill(dir, false); err != nil {
		t.Fatalf("fresh write: %v", err)
	}

	// Generated copy: an older generated body (different bytes, header
	// kept) refreshes without --force.
	skill := filepath.Join(dir, hostSkillDir, "SKILL.md")
	older := "---\n" + skillGenHeader + "\nname: gofastr-host\ndescription: old\n---\n\nOld body.\n"
	if err := os.WriteFile(skill, []byte(older), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteHostSkill(dir, false); err != nil {
		t.Fatalf("refresh of a generated copy: %v", err)
	}
	got, err := os.ReadFile(skill)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) == older {
		t.Error("generated copy was not refreshed")
	}
	if !strings.HasPrefix(string(got), "---\n"+skillGenHeader) {
		t.Errorf("refreshed copy lost its generated header")
	}
}

// A generated copy checked out with CRLF line endings (autocrlf, a
// Windows write) is still this command's own output: the header check
// is line-ending tolerant, so a refresh works without --force (review
// finding on #669 — the check would have refused its own previous
// output there).
func TestWriteHostSkillRefreshesCRLFCopy(t *testing.T) {
	dir := t.TempDir()
	skill := filepath.Join(dir, hostSkillDir, "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skill), 0o755); err != nil {
		t.Fatal(err)
	}
	lf, err := os.ReadFile(filepath.Join("embedded", "gofastr-host-skill.md"))
	if err != nil {
		t.Fatal(err)
	}
	crlf := strings.ReplaceAll(string(lf), "\n", "\r\n")
	if err := os.WriteFile(skill, []byte(crlf), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteHostSkill(dir, false); err != nil {
		t.Fatalf("CRLF copy of the generated skill was refused: %v", err)
	}
	got, err := os.ReadFile(skill)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(got), "---\n"+skillGenHeader) {
		t.Errorf("refreshed copy lost its generated header")
	}
}
