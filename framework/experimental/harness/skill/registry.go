package skill

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/skill/skillmd"
)

// Registry indexes skills by name: Load reads name and description, Activate the body, SupportingFile the tier-3 files.
type Registry struct {
	mu          sync.RWMutex
	skills      map[string]*entry
	searchPaths []string
}

type entry struct {
	tier1      skillmd.Tier1
	path       string
	sha256     string
	body       string
	bodyLoaded bool
	triggers   []string
	dir        string
}

// NewRegistry returns a Registry over searchPaths; a later path's skill replaces an earlier one of the same name.
func NewRegistry(searchPaths ...string) *Registry {
	return &Registry{
		skills:      make(map[string]*entry),
		searchPaths: searchPaths,
	}
}

// Load rescans the search paths, warning on stderr when a skill shadows another, and joins per-file errors.
func (r *Registry) Load() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.skills = make(map[string]*entry)
	var errs []error
	for _, root := range r.searchPaths {
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return nil
			}
			if d == nil || d.IsDir() {
				return nil
			}
			if filepath.Base(path) != "SKILL.md" {
				return nil
			}
			s, err := parseSkillRooted(path)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", path, err))
				return nil
			}
			if prev, exists := r.skills[s.Name]; exists && prev.path != path {
				fmt.Fprintf(os.Stderr,
					"skill: warning: %q from %s shadows the same-named skill from %s; the later path wins — verify both before trusting it\n",
					s.Name, path, prev.path)
			}
			r.skills[s.Name] = &entry{
				tier1:    s.Tier1(),
				path:     path,
				sha256:   s.SHA256,
				triggers: s.Triggers,
				dir:      s.Dir,
			}
			return nil
		})
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

func parseSkillRooted(path string) (*skillmd.Skill, error) {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, fmt.Errorf("skillmd: %s: %w", path, err)
	}
	defer root.Close()
	data, err := root.ReadFile(filepath.Base(path))
	if err != nil {
		return nil, fmt.Errorf("skillmd: %s: %w", path, err)
	}
	s, err := skillmd.ParseBytes(data)
	if err != nil {
		return nil, fmt.Errorf("skillmd: %s: %w", path, err)
	}
	s.Dir = filepath.Dir(path)
	sum := sha256.Sum256(data)
	s.SHA256 = hex.EncodeToString(sum[:])
	return s, nil
}

// Tier1Catalog returns every loaded skill's name and description, sorted by name.
func (r *Registry) Tier1Catalog() []skillmd.Tier1 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]skillmd.Tier1, 0, len(r.skills))
	for _, e := range r.skills {
		out = append(out, e.tier1)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Activate returns a skill's body, read from disk on the first call and cached after.
func (r *Registry) Activate(name string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.skills[name]
	if !ok {
		return "", fmt.Errorf("skill %q not loaded", name)
	}
	if e.bodyLoaded {
		return e.body, nil
	}
	parsed, err := parseSkillRooted(e.path)
	if err != nil {
		return "", err
	}
	e.body = parsed.Body
	e.bodyLoaded = true
	return e.body, nil
}

// SupportingFile reads rel inside the named skill's directory and refuses any path or symlink that leaves it.
func (r *Registry) SupportingFile(name, rel string) ([]byte, error) {
	r.mu.RLock()
	e, ok := r.skills[name]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("skill %q not loaded", name)
	}
	clean := filepath.Clean(rel)
	if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
		return nil, fmt.Errorf("supporting file path %q escapes skill directory", rel)
	}
	root, err := os.OpenRoot(e.dir)
	if err != nil {
		return nil, fmt.Errorf("skill %q: open dir %q: %w", name, e.dir, err)
	}
	defer root.Close()
	return root.ReadFile(clean)
}

// SHA256 returns the hex SHA-256 of the named skill's SKILL.md.
func (r *Registry) SHA256(name string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.skills[name]
	if !ok {
		return "", false
	}
	return e.sha256, true
}

// Names returns the loaded skill names in sorted order.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return slices.Sorted(maps.Keys(r.skills))
}

// MatchesTrigger returns, sorted, the skills with a trigger glob (holding / or *) matching input or a keyword in it, case-insensitive.
func (r *Registry) MatchesTrigger(input string) []string {
	if input == "" {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []string
	low := strings.ToLower(input)
	for _, e := range r.skills {
		for _, t := range e.triggers {
			if t == "" {
				continue
			}
			if strings.Contains(t, "/") || strings.Contains(t, "*") {
				if ok, _ := filepath.Match(t, input); ok {
					out = append(out, e.tier1.Name)
					break
				}
			} else if strings.Contains(low, strings.ToLower(t)) {
				out = append(out, e.tier1.Name)
				break
			}
		}
	}
	sort.Strings(out)
	return out
}
