package skillmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/markdown"
)

// Skill is a parsed SKILL.md.
type Skill struct {
	Name        string
	Description string
	Triggers    []string
	Body        string
	Dir         string
	SHA256      string
	Extra       map[string]string
}

// Tier1 is a skill's name and description.
type Tier1 struct {
	Name        string
	Description string
}

// Tier1 returns the skill's name and description.
func (s *Skill) Tier1() Tier1 {
	return Tier1{Name: s.Name, Description: s.Description}
}

// Parse reads and parses the SKILL.md at path, setting Dir and SHA256.
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

// ParseBytes parses SKILL.md content: "---" frontmatter, then the body.
func ParseBytes(data []byte) (*Skill, error) {
	src := string(data)
	if !strings.HasPrefix(src, "---") {
		return nil, errors.New("missing frontmatter (file must start with '---')")
	}
	rest := src[3:]
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

// Validate checks name (1-64 of a-z, 0-9, -) and description (1-1024 bytes).
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
		before, after, ok := strings.Cut(line, ":")
		if !ok {
			return fmt.Errorf("frontmatter: line %d missing ':' (%q)", i+1, line)
		}
		key := strings.TrimSpace(before)
		val := strings.TrimSpace(after)
		val = markdown.Unquote(val)
		if val == "" {
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

func readList(rest []string) (items []string, consumed int) {
	for i, line := range rest {
		trim := strings.TrimLeft(line, " \t")
		if !strings.HasPrefix(trim, "- ") {
			if items == nil {
				return nil, 0
			}
			return items, i
		}
		items = append(items, markdown.Unquote(strings.TrimSpace(trim[2:])))
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
		s.Extra[key] = strings.Join(items, ",")
	}
	return nil
}
