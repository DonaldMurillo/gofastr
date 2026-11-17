// Package semcov records what a test run reached into a coverage manifest.
package semcov

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/DonaldMurillo/gofastr/internal/coverageroot"
	"github.com/DonaldMurillo/gofastr/internal/filelock"
	"github.com/DonaldMurillo/gofastr/internal/fileperm"
)

// FileName is the manifest's path under the coverage root; Version is its schema.
const (
	FileName = ".gofastr/semantic-coverage.json"
	Version  = 1
)

const lockName = ".gofastr/semantic-coverage.lock"

// Manifest is what a test run touched; Permissions lists every permission
// evaluated, whatever the verdict.
type Manifest struct {
	Version int `json:"version"`
	// Routes maps a route pattern to the HTTP methods served through it.
	Routes      map[string][]string `json:"routes"`
	Permissions []string            `json:"permissions"`
	// Entities maps an entity name to the CRUD operations exercised.
	Entities map[string][]string `json:"entities"`
	// Hooks maps an entity name to the lifecycle hooks that fired.
	Hooks map[string][]string `json:"hooks,omitempty"`
	// Events are the event types published during the run.
	Events []string `json:"events,omitempty"`
	// Roles are the roles a caller held during a permission check.
	Roles []string `json:"roles,omitempty"`
}

func newManifest() *Manifest {
	return &Manifest{
		Version:  Version,
		Routes:   map[string][]string{},
		Entities: map[string][]string{},
		Hooks:    map[string][]string{},
	}
}

var (
	enabled atomic.Bool
	mu      sync.Mutex
	current *Manifest
	dir     string
	dirty   bool
)

// Enable turns recording on into root, [DefaultDir] for "", keeping data.
func Enable(root string) {
	mu.Lock()
	defer mu.Unlock()
	if root == "" {
		root = DefaultDir()
	}
	dir = root
	if current == nil {
		if existing, err := readFrom(root); err == nil {
			current = existing
		} else {
			current = newManifest()
		}
	}
	enabled.Store(true)
}

// Enabled reports whether recording is on.
func Enabled() bool { return enabled.Load() }

// Disable stops recording and keeps what was recorded for [Flush].
func Disable() { enabled.Store(false) }

// RecordRoute notes that a request was served by the route pattern.
func RecordRoute(method, pattern string) {
	if !enabled.Load() || pattern == "" {
		return
	}
	method = strings.ToUpper(strings.TrimSpace(method))
	mu.Lock()
	defer mu.Unlock()
	if current == nil || !enabled.Load() {
		return
	}
	if addTo(current.Routes, normalizePattern(pattern), method) {
		dirty = true
	}
}

// RecordPermission notes that a permission was evaluated.
func RecordPermission(permission string) {
	if !enabled.Load() {
		return
	}
	recordTrimmed(permission, func(manifest *Manifest) *[]string { return &manifest.Permissions })
}

// RecordEntityOp notes that an entity's CRUD operation ran.
func RecordEntityOp(entity, op string) {
	if !enabled.Load() || entity == "" || op == "" {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	if current == nil || !enabled.Load() {
		return
	}
	if addTo(current.Entities, strings.ToLower(entity), strings.ToLower(op)) {
		dirty = true
	}
}

// RecordRole notes that a caller held a role during a permission check.
func RecordRole(role string) {
	if !enabled.Load() {
		return
	}
	recordTrimmed(role, func(manifest *Manifest) *[]string { return &manifest.Roles })
}

// RecordEvent notes that an event type was published on the bus.
func RecordEvent(eventType string) {
	if !enabled.Load() {
		return
	}
	recordTrimmed(eventType, func(manifest *Manifest) *[]string { return &manifest.Events })
}

func recordTrimmed(value string, field func(*Manifest) *[]string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	if current == nil || !enabled.Load() {
		return
	}
	dst := field(current)
	if !slices.Contains(*dst, value) {
		*dst = append(*dst, value)
		slices.Sort(*dst)
		dirty = true
	}
}

// RecordHook notes that a registered lifecycle hook ran for an entity.
func RecordHook(entity, hookType string) {
	if !enabled.Load() || entity == "" || hookType == "" {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	if current == nil || !enabled.Load() {
		return
	}
	if addTo(current.Hooks, strings.ToLower(entity), strings.ToLower(hookType)) {
		dirty = true
	}
}

func addTo(m map[string][]string, key, value string) bool {
	if key == "" || value == "" {
		return false
	}
	if slices.Contains(m[key], value) {
		return false
	}
	m[key] = append(m[key], value)
	slices.Sort(m[key])
	return true
}

// Flush merges what was recorded since the last write into the manifest.
func Flush() error {
	mu.Lock()
	defer mu.Unlock()
	if current == nil || dir == "" || !dirty {
		return nil
	}
	if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(lockName)), 0o700); err != nil {
		return fmt.Errorf("semcov: create %s: %w", filepath.Dir(lockName), err)
	}
	lock, err := os.OpenFile(filepath.Join(dir, lockName), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("semcov: open lock: %w", err)
	}
	defer lock.Close()
	if err := filelock.Lock(lock); err != nil {
		return fmt.Errorf("semcov: lock manifest: %w", err)
	}
	merged := current
	if onDisk, err := readFrom(dir); err == nil {
		merged = mergeManifests(onDisk, current)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := writeTo(dir, merged); err != nil {
		return err
	}
	current, dirty = merged, false
	return nil
}

func mergeManifests(a, b *Manifest) *Manifest {
	out := newManifest()
	for _, src := range []*Manifest{a, b} {
		if src == nil {
			continue
		}
		for k, vals := range src.Routes {
			for _, v := range vals {
				addTo(out.Routes, k, v)
			}
		}
		for k, vals := range src.Entities {
			for _, v := range vals {
				addTo(out.Entities, k, v)
			}
		}
		for k, vals := range src.Hooks {
			for _, v := range vals {
				addTo(out.Hooks, k, v)
			}
		}
		for _, perm := range src.Permissions {
			if !slices.Contains(out.Permissions, perm) {
				out.Permissions = append(out.Permissions, perm)
			}
		}
		for _, evt := range src.Events {
			if !slices.Contains(out.Events, evt) {
				out.Events = append(out.Events, evt)
			}
		}
		for _, role := range src.Roles {
			if !slices.Contains(out.Roles, role) {
				out.Roles = append(out.Roles, role)
			}
		}
	}
	slices.Sort(out.Permissions)
	slices.Sort(out.Events)
	slices.Sort(out.Roles)
	return out
}

func writeTo(root string, m *Manifest) error {
	file := filepath.Join(root, FileName)
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return fmt.Errorf("semcov: create %s: %w", filepath.Dir(file), err)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("semcov: encode manifest: %w", err)
	}
	tmp := fmt.Sprintf("%s.%d.tmp", file, os.Getpid())
	if err := fileperm.WriteOwnerOnly(tmp, append(data, '\n')); err != nil {
		return fmt.Errorf("semcov: write manifest: %w", err)
	}
	if err := os.Rename(tmp, file); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("semcov: replace manifest: %w", err)
	}
	return nil
}

// Read loads the manifest under root; a missing one is fs.ErrNotExist.
func Read(root string) (*Manifest, error) { return readFrom(root) }

func readFrom(root string) (*Manifest, error) {
	data, err := os.ReadFile(filepath.Join(root, FileName))
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("semcov: parse %s: %w", FileName, err)
	}
	if m.Version > Version {
		return nil, fmt.Errorf("semcov: manifest is version %d, this build understands %d: delete %s and re-run the tests",
			m.Version, Version, FileName)
	}
	if m.Routes == nil {
		m.Routes = map[string][]string{}
	}
	if m.Entities == nil {
		m.Entities = map[string][]string{}
	}
	if m.Hooks == nil {
		m.Hooks = map[string][]string{}
	}
	return &m, nil
}

// Reset clears what was recorded and turns recording off, for tests.
func Reset() {
	mu.Lock()
	defer mu.Unlock()
	current = nil
	dir = ""
	dirty = false
	enabled.Store(false)
}

// CoveredRoute reports whether method was served through pattern.
func (m *Manifest) CoveredRoute(method, pattern string) bool {
	if m == nil {
		return false
	}
	return slices.Contains(m.Routes[normalizePattern(pattern)], strings.ToUpper(method))
}

// CoveredEntity reports whether any operation ran on the entity.
func (m *Manifest) CoveredEntity(name string) bool {
	if m == nil {
		return false
	}
	return len(m.Entities[strings.ToLower(name)]) > 0
}

// CoveredHook reports whether an entity's lifecycle hook fired.
func (m *Manifest) CoveredHook(entity, hookType string) bool {
	if m == nil {
		return false
	}
	return slices.Contains(m.Hooks[strings.ToLower(entity)], strings.ToLower(hookType))
}

// CoveredEvent reports whether an event type was published.
func (m *Manifest) CoveredEvent(eventType string) bool {
	if m == nil {
		return false
	}
	return slices.Contains(m.Events, eventType)
}

// CoveredRole reports whether a caller ever held the role during a check.
func (m *Manifest) CoveredRole(role string) bool {
	if m == nil {
		return false
	}
	return slices.Contains(m.Roles, role)
}

// CoveredPermission reports whether a permission was evaluated.
func (m *Manifest) CoveredPermission(p string) bool {
	if m == nil {
		return false
	}
	return slices.Contains(m.Permissions, p)
}

func normalizePattern(p string) string {
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		p = p[:i]
	}
	if len(p) > 1 {
		p = strings.TrimSuffix(p, "/")
	}
	if p == "" {
		return "/"
	}
	return p
}

// DirFor is the coverage root a writer in the project at root resolves.
func DirFor(root string) string {
	if v := overrideDir(); v != "" {
		return v
	}
	start := root
	if abs, err := filepath.Abs(root); err == nil {
		start = abs
	}
	if d, ok := coverageroot.FindUp(start, "go.work"); ok {
		return d
	}
	return root
}

var overrideKeys = []string{"GOFASTR_SEMANTIC_COVERAGE_DIR", "GOFASTR_AXE_COVERAGE_DIR"}

func overrideDir() string {
	for _, key := range overrideKeys {
		if v := os.Getenv(key); v != "" {
			return v
		}
	}
	return ""
}

// DefaultDir is GOFASTR_SEMANTIC_COVERAGE_DIR, else axecov.DefaultDir.
func DefaultDir() string {
	return coverageroot.Resolve(overrideKeys...)
}
