// Package skillmd parses SKILL.md files per the open spec at
// https://agentskills.io/specification.
//
// Frontmatter: YAML between "---" delimiters. Required fields: name
// (≤64 chars, lowercase + hyphens) and description (≤1024 chars).
// Optional: triggers (list of strings).
//
// We parse YAML by hand for the tiny subset SKILL.md uses
// (frontmatter is shallow: scalar strings and string lists).
package skillmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Skill is the parsed representation of a SKILL.md file (tier-1 +
// tier-2 from the progressive-disclosure model).
type Skill struct {
	Name        string
	Description string
	Triggers    []string
	Body        string // markdown after frontmatter
	Dir         string // absolute path to the directory containing SKILL.md
	SHA256      string // hash of the SKILL.md file for TOFU
	Extra       map[string]string // pass-through frontmatter fields
}

// Tier1 returns just the name + description; this is what the loader
// surfaces at startup (the ~100 tokens/skill cost the spec markets).
type Tier1 struct {
	Name        string
	Description string
}

// Tier1 returns the lightweight metadata.
func (s *Skill) Tier1() Tier1 {
	return Tier1{Name: s.Name, Description: s.Description}
}

// Parse reads a SKILL.md file at the given path.
func Parse(path string) (*Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("skillmd: %w", err)
	}
	s, err := ParseBytes(data)
	if err != nil {
		return nil, fmt.Errorf("skillmd: %s: %w", path, err)
	}
	s.Dir = filepath.Dir(path)
	s.SHA256 = sha256Hex(data)
	return s, nil
}

// ParseBytes parses SKILL.md content directly.
func ParseBytes(data []byte) (*Skill, error) {
	src := string(data)
	// Frontmatter required.
	if !strings.HasPrefix(src, "---") {
		return nil, errors.New("missing frontmatter (file must start with '---')")
	}
	rest := src[3:]
	// Skip optional newline after opening fence.
	rest = strings.TrimPrefix(rest, "\n")
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return nil, errors.New("unterminated frontmatter")
	}
	front := rest[:end]
	body := rest[end+len("\n---"):]
	body = strings.TrimPrefix(body, "\n")

	s := &Skill{Extra: map[string]string{}}
	if err := parseFrontmatter(front, s); err != nil {
		return nil, err
	}
	s.Body = body
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return s, nil
}

// Validate enforces the spec's required-field constraints.
func (s *Skill) Validate() error {
	if s.Name == "" {
		return errors.New("frontmatter missing required 'name'")
	}
	if len(s.Name) > 64 {
		return fmt.Errorf("name length %d exceeds 64 chars", len(s.Name))
	}
	if !validNameChars(s.Name) {
		return fmt.Errorf("name %q contains invalid characters (allowed: a-z 0-9 -)", s.Name)
	}
	if s.Description == "" {
		return errors.New("frontmatter missing required 'description'")
	}
	if len(s.Description) > 1024 {
		return fmt.Errorf("description length %d exceeds 1024 chars", len(s.Description))
	}
	return nil
}

func validNameChars(s string) bool {
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-':
		default:
			return false
		}
	}
	return true
}

// parseFrontmatter handles the YAML subset SKILL.md uses: top-level
// `key: value` pairs and `key:` followed by `- item` lines.
func parseFrontmatter(src string, s *Skill) error {
	lines := strings.Split(src, "\n")
	for i := 0; i < len(lines); i++ {
		raw := lines[i]
		line := strings.TrimRight(raw, " \r")
		if line == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if !startsAtCol0(line) {
			continue
		}
		colon := strings.Index(line, ":")
		if colon < 0 {
			return fmt.Errorf("frontmatter: line %d missing ':' (%q)", i+1, line)
		}
		key := strings.TrimSpace(line[:colon])
		val := strings.TrimSpace(line[colon+1:])
		// Strip surrounding quotes on scalars.
		val = trimYAMLQuotes(val)
		if val == "" {
			// Either a list follows, or it's just empty.
			items, consumed := readList(lines[i+1:])
			i += consumed
			if items != nil {
				if err := assignList(s, key, items); err != nil {
					return err
				}
			} else {
				if err := assignScalar(s, key, ""); err != nil {
					return err
				}
			}
			continue
		}
		if err := assignScalar(s, key, val); err != nil {
			return err
		}
	}
	return nil
}

func startsAtCol0(s string) bool {
	if len(s) == 0 {
		return false
	}
	return s[0] != ' ' && s[0] != '\t'
}

func trimYAMLQuotes(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// readList consumes `- item` lines from the start of the slice.
// Returns the items (nil if no list found) and how many lines were
// consumed.
func readList(rest []string) (items []string, consumed int) {
	for i, line := range rest {
		trim := strings.TrimLeft(line, " \t")
		if !strings.HasPrefix(trim, "- ") {
			if items == nil {
				return nil, 0
			}
			return items, i
		}
		items = append(items, trimYAMLQuotes(strings.TrimSpace(trim[2:])))
		consumed = i + 1
	}
	return items, consumed
}

func assignScalar(s *Skill, key, val string) error {
	switch key {
	case "name":
		s.Name = val
	case "description":
		s.Description = val
	default:
		s.Extra[key] = val
	}
	return nil
}

func assignList(s *Skill, key string, items []string) error {
	switch key {
	case "triggers":
		s.Triggers = items
	default:
		// Store joined for round-trip visibility.
		s.Extra[key] = strings.Join(items, ",")
	}
	return nil
}
