// Package semcov is the semantic-coverage manifest: a record of what a
// test run actually exercised, as opposed to which lines it happened to
// execute.
//
// Line coverage answers "did this statement run". It cannot answer the
// questions that decide whether a change is safe:
//
//   - Did a request ever reach this route through the real router, the
//     real middleware chain, and the real auth check?
//   - Was this permission ever evaluated, not just present in a file
//     that got loaded?
//   - Did anything ever call this entity's Delete endpoint?
//
// A handler can sit at 100% line coverage because a unit test called the
// function directly, while no request has ever reached it through the
// router. That handler is untested in every way that matters, and the
// coverage number says the opposite.
//
// # How it works
//
// The shape is borrowed wholesale from [framework/axecov], which solves
// the same problem for accessibility scans and has the operational
// behaviour already proven: tests write, tooling reads, the file lives
// under `.gofastr/` (gitignored, wiped by `make clean`) so it never
// ships, and both sides resolve the same directory through [DefaultDir]
// even when their working directories differ.
//
// Recording is opt-in per process and free when off. `framework/testkit`
// turns it on for the app under test; production binaries never call
// [Enable], so the hooks compile to an atomic load and a branch.
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
)

// FileName is the manifest location relative to the coverage root.
const FileName = ".gofastr/semantic-coverage.json"

// Version is the manifest schema version. A reader that finds a higher
// version reports the mismatch rather than guessing at the contents.
const Version = 1

// Manifest is what a test run proved it touched.
type Manifest struct {
	Version int `json:"version"`
	// Routes maps a route pattern to the HTTP methods exercised against
	// it. Keyed by *pattern*, not by request path, so one test hitting
	// /orders/42 credits the /orders/{id} route.
	Routes map[string][]string `json:"routes"`
	// Permissions are the permission strings that were evaluated,
	// whatever the verdict, a denial proves the boundary as well as a
	// grant does, and arguably better.
	Permissions []string `json:"permissions"`
	// Entities maps an entity name to the CRUD operations exercised
	// (list, get, create, update, delete).
	Entities map[string][]string `json:"entities"`
	// Hooks maps an entity name to the lifecycle hooks that actually
	// fired (beforecreate, afterupdate, …). A registered hook that never
	// runs is the classic silent break: the code is there, the tests are
	// green, and the behaviour it adds has never happened.
	Hooks map[string][]string `json:"hooks,omitempty"`
	// Events are the event types actually published during the run. A
	// subscriber registered for a type that never appears here has never
	// run, the same silent break as an unfired hook, one layer out.
	Events []string `json:"events,omitempty"`
	// Roles are the roles a caller actually held during a recorded
	// permission check. A granted role nothing ever authenticates as is an
	// authorization path no test has walked.
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

// recorder is the process-wide accumulator. Recording is off until
// [Enable] is called, so the hooks cost one atomic load in production.
var (
	enabled atomic.Bool
	mu      sync.Mutex
	current *Manifest
	dir     string
	// dirty tracks whether anything new arrived since the last write, so
	// a per-test Flush is free when that test recorded nothing new. Test
	// binaries can hold thousands of tests; re-serialising the manifest
	// for each one would be a real cost for no information.
	dirty bool
)

// Enable turns recording on and points it at a coverage root. Passing ""
// uses [DefaultDir]. Calling it twice is safe: the accumulated data is
// kept, so two test packages in one binary do not erase each other.
func Enable(root string) {
	mu.Lock()
	defer mu.Unlock()
	if root == "" {
		root = DefaultDir()
	}
	dir = root
	if current == nil {
		// Start from what is already on disk so a `go test ./...` run
		// across many packages, each its own process, accumulates
		// rather than each package overwriting the last.
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

// Disable stops recording. Data already accumulated is retained so a
// deferred [Flush] still writes it.
func Disable() { enabled.Store(false) }

// RecordRoute notes that a request was served by the given route pattern.
// The pattern is the router's registered pattern, not the request path.
func RecordRoute(method, pattern string) {
	if !enabled.Load() || pattern == "" {
		return
	}
	method = strings.ToUpper(strings.TrimSpace(method))
	mu.Lock()
	defer mu.Unlock()
	if addTo(current.Routes, normalizePattern(pattern), method) {
		dirty = true
	}
}

// RecordPermission notes that a permission was evaluated.
func RecordPermission(permission string) {
	if !enabled.Load() {
		return
	}
	permission = strings.TrimSpace(permission)
	if permission == "" {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	if !slices.Contains(current.Permissions, permission) {
		current.Permissions = append(current.Permissions, permission)
		slices.Sort(current.Permissions)
		dirty = true
	}
}

// RecordEntityOp notes that an entity's CRUD operation ran. op is one of
// list, get, create, update, delete.
func RecordEntityOp(entity, op string) {
	if !enabled.Load() || entity == "" || op == "" {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	if addTo(current.Entities, strings.ToLower(entity), strings.ToLower(op)) {
		dirty = true
	}
}

// RecordRole notes that a caller held a role during a permission check.
func RecordRole(role string) {
	if !enabled.Load() {
		return
	}
	role = strings.TrimSpace(role)
	if role == "" {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	if !slices.Contains(current.Roles, role) {
		current.Roles = append(current.Roles, role)
		slices.Sort(current.Roles)
		dirty = true
	}
}

// RecordEvent notes that an event type was published on the bus.
func RecordEvent(eventType string) {
	if !enabled.Load() {
		return
	}
	eventType = strings.TrimSpace(eventType)
	if eventType == "" {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	if !slices.Contains(current.Events, eventType) {
		current.Events = append(current.Events, eventType)
		slices.Sort(current.Events)
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
	if addTo(current.Hooks, strings.ToLower(entity), strings.ToLower(hookType)) {
		dirty = true
	}
}

// addTo inserts value under key, reporting whether it was new.
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

// Flush writes the accumulated manifest. It merges with whatever is on
// disk first, because `go test ./...` runs one process per package and
// each one only knows about its own requests, without the merge, the
// last package to finish would be the only one recorded.
func Flush() error {
	mu.Lock()
	defer mu.Unlock()
	if current == nil || dir == "" || !dirty {
		return nil
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
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return fmt.Errorf("semcov: create %s: %w", filepath.Dir(file), err)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("semcov: encode manifest: %w", err)
	}
	// Write-then-rename so a concurrent reader never sees a torn file.
	// The temp name carries the pid because `go test ./...` runs packages
	// in parallel processes, and a shared "manifest.tmp" would let two of
	// them rename each other's half-written file into place.
	tmp := fmt.Sprintf("%s.%d.tmp", file, os.Getpid())
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("semcov: write manifest: %w", err)
	}
	if err := os.Rename(tmp, file); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("semcov: replace manifest: %w", err)
	}
	return nil
}

// Read loads the manifest under root. A missing manifest returns an error
// satisfying errors.Is(err, fs.ErrNotExist), so callers can distinguish
// "never recorded" (fine on a fresh clone) from "recorded and incomplete"
// (real drift).
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

// Reset clears in-process state. For tests of this package and of the
// harness that drives it.
func Reset() {
	mu.Lock()
	defer mu.Unlock()
	current = nil
	dir = ""
	dirty = false
	enabled.Store(false)
}

// CoveredRoute reports whether the manifest records the method against
// the pattern.
func (m *Manifest) CoveredRoute(method, pattern string) bool {
	if m == nil {
		return false
	}
	return slices.Contains(m.Routes[normalizePattern(pattern)], strings.ToUpper(method))
}

// CoveredEntity reports whether the manifest records any operation on an
// entity.
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

// normalizePattern strips a query string and trailing slash so the writer
// and the reader agree on a key. The root stays "/".
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

// DefaultDir resolves the coverage root the same way [framework/axecov]
// does, so a project has one place where its coverage artifacts live:
//
//  1. GOFASTR_SEMANTIC_COVERAGE_DIR, when set.
//  2. GOFASTR_AXE_COVERAGE_DIR, when set: sharing the override means an
//     app that already pinned its axe manifest per-app gets the same
//     isolation here without configuring it twice.
//  3. The nearest ancestor holding go.work (Go's own workspace rule).
//  4. Else the nearest ancestor holding go.mod.
//  5. Else the working directory.
func DefaultDir() string {
	for _, key := range []string{"GOFASTR_SEMANTIC_COVERAGE_DIR", "GOFASTR_AXE_COVERAGE_DIR"} {
		if v := os.Getenv(key); v != "" {
			return v
		}
	}
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	for _, marker := range []string{"go.work", "go.mod"} {
		for d := wd; ; {
			if _, err := os.Stat(filepath.Join(d, marker)); err == nil {
				return d
			}
			parent := filepath.Dir(d)
			if parent == d {
				break
			}
			d = parent
		}
	}
	return wd
}
