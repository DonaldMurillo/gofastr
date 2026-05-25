// Package memory implements file-backed auto-memory matching the
// markdown+frontmatter pattern documented in
// docs/harness-architecture.md § Memory.
//
// Layout under the memory root:
//
//	MEMORY.md                index ("- [Title](file.md) — hook")
//	user_role.md             one file per memory
//	feedback_testing.md
//	project_harness.md
//
// Frontmatter required: name, description, metadata.type. Four
// types: user, feedback, project, reference.
package memory

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Type is the kind of memory.
type Type string

const (
	TypeUser      Type = "user"
	TypeFeedback  Type = "feedback"
	TypeProject   Type = "project"
	TypeReference Type = "reference"
)

// Entry is one memory file.
type Entry struct {
	Name        string // slug, used as filename minus ".md"
	Description string
	Type        Type
	Body        string
	Path        string // absolute path on disk
}

// Store is the file-backed memory store.
type Store struct {
	mu   sync.RWMutex
	root string
	by   map[string]*Entry // by name
}

// New returns a Store rooted at the given directory. The directory is
// created if it doesn't exist.
func New(root string) (*Store, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	s := &Store{root: root, by: make(map[string]*Entry)}
	if err := s.reload(); err != nil {
		return nil, err
	}
	return s, nil
}

// All returns every loaded entry sorted by name.
func (s *Store) All() []*Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Entry, 0, len(s.by))
	for _, e := range s.by {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ByType returns entries of one type.
func (s *Store) ByType(t Type) []*Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*Entry
	for _, e := range s.by {
		if e.Type == t {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Get returns a memory by name, or false.
func (s *Store) Get(name string) (*Entry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.by[name]
	return e, ok
}

// Save writes (or overwrites) a memory and updates the index.
func (s *Store) Save(e Entry) error {
	if err := validate(&e); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e.Path = filepath.Join(s.root, e.Name+".md")
	content := serialize(&e)
	if err := os.WriteFile(e.Path, []byte(content), 0o644); err != nil {
		return err
	}
	s.by[e.Name] = &e
	return s.writeIndexLocked()
}

// Delete removes a memory.
func (s *Store) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.by[name]
	if !ok {
		return nil
	}
	_ = os.Remove(e.Path)
	delete(s.by, name)
	return s.writeIndexLocked()
}

// Relevant scores every entry against a query string and returns the
// top n entries. Scoring is a simple tag/keyword overlap; sufficient
// for the engine middleware's "inject relevant memories per turn" step.
func (s *Store) Relevant(query string, n int) []*Entry {
	if query == "" || n <= 0 {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	q := strings.ToLower(query)
	type scored struct {
		e     *Entry
		score int
	}
	var out []scored
	for _, e := range s.by {
		hay := strings.ToLower(e.Name + " " + e.Description + " " + e.Body)
		score := 0
		// Each keyword in the query that appears in the haystack gains 1 point;
		// matching the name or description gains 3.
		for _, tok := range tokenize(q) {
			if strings.Contains(strings.ToLower(e.Name), tok) || strings.Contains(strings.ToLower(e.Description), tok) {
				score += 3
				continue
			}
			if strings.Contains(hay, tok) {
				score++
			}
		}
		if score > 0 {
			out = append(out, scored{e, score})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].score != out[j].score {
			return out[i].score > out[j].score
		}
		return out[i].e.Name < out[j].e.Name
	})
	if n > len(out) {
		n = len(out)
	}
	picked := make([]*Entry, 0, n)
	for i := 0; i < n; i++ {
		picked = append(picked, out[i].e)
	}
	return picked
}

func tokenize(s string) []string {
	var out []string
	cur := strings.Builder{}
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '.' || r == ',' || r == ';' || r == ':' {
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		} else {
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

func (s *Store) reload() error {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return err
	}
	for _, de := range entries {
		if de.IsDir() {
			continue
		}
		name := de.Name()
		if name == "MEMORY.md" || !strings.HasSuffix(name, ".md") {
			continue
		}
		p := filepath.Join(s.root, name)
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		e, err := parse(string(raw))
		if err != nil {
			return fmt.Errorf("memory: %s: %w", p, err)
		}
		e.Path = p
		s.by[e.Name] = e
	}
	return nil
}

func (s *Store) writeIndexLocked() error {
	var b strings.Builder
	entries := make([]*Entry, 0, len(s.by))
	for _, e := range s.by {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	for _, e := range entries {
		fmt.Fprintf(&b, "- [%s](%s.md) — %s\n", e.Name, e.Name, e.Description)
	}
	return os.WriteFile(filepath.Join(s.root, "MEMORY.md"), []byte(b.String()), 0o644)
}

func validate(e *Entry) error {
	if e.Name == "" {
		return errors.New("memory: name required")
	}
	if e.Description == "" {
		return errors.New("memory: description required")
	}
	switch e.Type {
	case TypeUser, TypeFeedback, TypeProject, TypeReference:
	default:
		return fmt.Errorf("memory: invalid type %q", e.Type)
	}
	return nil
}

func serialize(e *Entry) string {
	body := e.Body
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	return fmt.Sprintf(`---
name: %s
description: %s
metadata:
  type: %s
---

%s`, e.Name, e.Description, e.Type, body)
}

func parse(raw string) (*Entry, error) {
	if !strings.HasPrefix(raw, "---") {
		return nil, errors.New("missing frontmatter")
	}
	rest := strings.TrimPrefix(raw, "---")
	rest = strings.TrimPrefix(rest, "\n")
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return nil, errors.New("unterminated frontmatter")
	}
	front := rest[:end]
	body := strings.TrimPrefix(rest[end+len("\n---"):], "\n")

	e := &Entry{Body: body}
	inMetadata := false
	for _, line := range strings.Split(front, "\n") {
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		// metadata: starts a sub-section; following indented lines
		// are its keys.
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			inMetadata = false
			colon := strings.Index(line, ":")
			if colon < 0 {
				continue
			}
			key := strings.TrimSpace(line[:colon])
			val := strings.TrimSpace(line[colon+1:])
			val = strings.Trim(val, `"'`)
			switch key {
			case "name":
				e.Name = val
			case "description":
				e.Description = val
			case "metadata":
				inMetadata = true
			}
			continue
		}
		if inMetadata {
			colon := strings.Index(line, ":")
			if colon < 0 {
				continue
			}
			key := strings.TrimSpace(line[:colon])
			val := strings.TrimSpace(line[colon+1:])
			if key == "type" {
				e.Type = Type(strings.Trim(val, `"'`))
			}
		}
	}
	if err := validate(e); err != nil {
		return nil, err
	}
	return e, nil
}
