// Package skill wraps the SKILL.md parser with the 3-tier
// progressive-disclosure machinery the engine uses.
//
// Tier 1: name + description loaded at startup (~100 tokens/skill).
// Tier 2: full SKILL.md body fetched on activation.
// Tier 3: supporting files in scripts/, references/, assets/ —
//         fetched only on explicit reference from tier 2.
package skill

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/framework/harness/skill/skillmd"
)

// Registry indexes available skills by name. At startup it scans
// configured search paths and loads tier-1 metadata for each. Tier-2
// bodies are loaded on first Activate.
type Registry struct {
	mu          sync.RWMutex
	skills      map[string]*entry
	searchPaths []string
}

type entry struct {
	tier1     skillmd.Tier1
	path      string // absolute path to SKILL.md
	sha256    string
	body      string // tier-2, loaded on activation
	bodyLoaded bool
	triggers  []string
	dir       string // skill directory containing scripts/ references/ assets/
}

// NewRegistry returns a Registry that scans the given search paths
// when Load is called. Paths are searched in order; later paths
// override earlier ones (project-local overrides global which
// overrides built-in).
func NewRegistry(searchPaths ...string) *Registry {
	return &Registry{
		skills:      make(map[string]*entry),
		searchPaths: searchPaths,
	}
}

// Load scans the search paths and populates tier-1 metadata. Errors
// reading individual files are returned as a joined error; loading
// continues across remaining files.
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
			s, err := skillmd.Parse(path)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", path, err))
				return nil
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

// Tier1Catalog returns the lightweight metadata for all loaded skills,
// sorted by name. Engine uses this for the system-prompt skill list.
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

// Activate returns the tier-2 body of a skill, loading it from disk
// on first call. Subsequent calls hit the in-memory cache.
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
	parsed, err := skillmd.Parse(e.path)
	if err != nil {
		return "", err
	}
	e.body = parsed.Body
	e.bodyLoaded = true
	return e.body, nil
}

// SupportingFile reads a tier-3 file (under scripts/, references/, or
// assets/) for the named skill. Refuses paths that escape the skill
// directory.
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
	return os.ReadFile(filepath.Join(e.dir, clean))
}

// SHA256 returns the hash of the named skill's SKILL.md, for TOFU comparison.
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
	names := make([]string, 0, len(r.skills))
	for n := range r.skills {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// MatchesTrigger returns the names of skills whose triggers match the
// given input (filename glob OR keyword pattern). Empty input
// returns nothing.
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
			// Filename glob (contains /) or keyword (substring match).
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
