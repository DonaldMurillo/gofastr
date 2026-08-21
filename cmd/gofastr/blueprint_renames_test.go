package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeBP writes a blueprint and loads it, returning the error for inspection.
func loadBPWithEntity(t *testing.T, entityYAML string) (Blueprint, error) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	bp := "app:\n  name: testapp\nentities:\n" + entityYAML
	if err := os.WriteFile(path, []byte(bp), 0o644); err != nil {
		t.Fatal(err)
	}
	return loadBlueprint(path)
}

// TestRenamesDecodeFromYAML: a rename declared in blueprint YAML reaches
// EntityConfig.Renames, so the schema diff can emit RENAME COLUMN instead of a
// data-losing drop+add. Renames were Go-declaration-only, which left the
// blueprint workflow, the only input the standalone migration generator reads,
// unable to express the difference.
func TestRenamesDecodeFromYAML(t *testing.T) {
	bp, err := loadBPWithEntity(t, `  - name: posts
    table: posts
    renames:
      headline: title
    fields:
      - name: title
        type: string
`)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(bp.Entities) != 1 {
		t.Fatalf("entities = %d, want 1", len(bp.Entities))
	}
	cfg, err := bp.Entities[0].Config()
	if err != nil {
		t.Fatalf("Config: %v", err)
	}
	if got := cfg.Renames["headline"]; got != "title" {
		t.Errorf("Renames[headline] = %q, want \"title\"", got)
	}
}

// TestRenamesRejectUnsafeIdentifier: both sides of a rename become column names
// in emitted ALTER TABLE DDL, so they must be safe identifiers: a blueprint is
// agent-transcribed text, not developer-authored SQL.
func TestRenamesRejectUnsafeIdentifier(t *testing.T) {
	for _, bad := range []string{
		`      "old\"; DROP TABLE posts; --": title`,
		`      headline: "title\"; DROP TABLE posts; --"`,
	} {
		_, err := loadBPWithEntity(t, `  - name: posts
    table: posts
    renames:
`+bad+`
    fields:
      - name: title
        type: string
`)
		if err == nil {
			t.Errorf("expected rejection for rename %s", strings.TrimSpace(bad))
			continue
		}
		// Must be rejected as an unsafe column name, not merely as an
		// unrecognised key. Otherwise this passes before the feature exists.
		if !strings.Contains(err.Error(), "not a safe column name") {
			t.Errorf("rename %s rejected for the wrong reason: %v", strings.TrimSpace(bad), err)
		}
	}
}

// TestRenamesTargetMustBeDeclared: a rename to a column the entity does not
// declare would never fire, so it is a typo worth catching at validate time.
func TestRenamesTargetMustBeDeclared(t *testing.T) {
	_, err := loadBPWithEntity(t, `  - name: posts
    table: posts
    renames:
      headline: nope
    fields:
      - name: title
        type: string
`)
	if err == nil {
		t.Fatal("expected a rename to an undeclared field to be rejected")
	}
	if !strings.Contains(err.Error(), "nope") {
		t.Errorf("error should name the undeclared target: %v", err)
	}
}

// TestAuthFooterHrefRejectedAtValidate: ui.Link degrades an unsafe footer href
// to a dead link at render time, which is safe but silent. The author gets a
// broken link instead of an error. Every sibling problem in validateBlueprint
// (theme values, relation names, screen layouts) fails the generate, so this
// one does too. It shares urlsafe.Anchor with the renderer, so validate and
// render cannot disagree about what is safe.
func TestAuthFooterHrefRejectedAtValidate(t *testing.T) {
	block := func(kind, key, href string) string {
		return `  - name: login
    route: /login
    body:
      - kind: ` + kind + `
        props:
          ` + key + `: "` + href + `"
`
	}
	for _, c := range []struct {
		name, kind, key, href string
		wantErr               bool
	}{
		{"login javascript", "login_form", "register_href", "javascript:fetch('//x')", true},
		{"signup javascript", "signup_form", "login_href", "javascript:alert(1)", true},
		{"login data uri", "login_form", "register_href", "data:text/html,<script>", true},
		{"relative ok", "login_form", "register_href", "/signup", false},
		{"mailto ok", "login_form", "register_href", "mailto:help@example.com", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "gofastr.yml")
			bp := "app:\n  name: testapp\nscreens:\n" + block(c.kind, c.key, c.href)
			if err := os.WriteFile(path, []byte(bp), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := loadBlueprint(path)
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected %q to be rejected", c.href)
				}
				if !strings.Contains(err.Error(), c.key) {
					t.Errorf("error should name the offending prop %q: %v", c.key, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("%q should be accepted: %v", c.href, err)
			}
		})
	}
}
