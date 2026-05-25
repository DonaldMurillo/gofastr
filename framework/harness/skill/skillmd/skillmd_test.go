package skillmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseBytesMinimal(t *testing.T) {
	src := `---
name: read-files
description: Reads files for the agent
---

This is the skill body.
`
	s, err := ParseBytes([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "read-files" {
		t.Errorf("name = %q", s.Name)
	}
	if s.Description != "Reads files for the agent" {
		t.Errorf("desc = %q", s.Description)
	}
	if !strings.Contains(s.Body, "skill body") {
		t.Errorf("body = %q", s.Body)
	}
}

func TestParseTriggers(t *testing.T) {
	src := `---
name: gofastr-ui
description: GoFastr UI architecture guidance
triggers:
  - "*.tsx"
  - "core-ui/**"
  - SSR
---

body
`
	s, err := ParseBytes([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Triggers) != 3 {
		t.Fatalf("triggers = %v", s.Triggers)
	}
	if s.Triggers[0] != "*.tsx" || s.Triggers[2] != "SSR" {
		t.Errorf("triggers[0]=%q triggers[2]=%q", s.Triggers[0], s.Triggers[2])
	}
}

func TestValidateRejectsBadName(t *testing.T) {
	src := `---
name: BadName_With_Underscores
description: x
---

body
`
	if _, err := ParseBytes([]byte(src)); err == nil {
		t.Fatal("expected error for invalid chars")
	}
}

func TestValidateRequiresName(t *testing.T) {
	src := `---
description: x
---

body
`
	if _, err := ParseBytes([]byte(src)); err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestParseFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	_ = os.WriteFile(path, []byte(`---
name: x
description: y
---

body
`), 0o644)
	s, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.SHA256 == "" {
		t.Error("missing sha256")
	}
	if s.Dir != dir {
		t.Errorf("Dir = %q, want %q", s.Dir, dir)
	}
}
