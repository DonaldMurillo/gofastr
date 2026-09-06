package desktop

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"runtime"
	"slices"
	"sync"
)

// Name grammars. Capability names are the JS namespace and the URL
// segment; method names are the JS function. Both are validated at
// Register time so nothing unvetted can reach a URL, the generated
// bridge, or the manifest.
var (
	reCapabilityName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	reMethodName     = regexp.MustCompile(`^[a-z][A-Za-z0-9_]{0,63}$`)
	rePermission     = regexp.MustCompile(`^[a-z][a-z0-9_-]*:[a-z][a-z0-9_-]*$`)
)

// Capability is one named group of bridge methods ("clipboard",
// "window", a plugin's own). Registered before Run freezes the
// registry; duplicate names are refused naming both registrants.
type Capability struct {
	Name        string
	Description string
	// Version is bumped on any breaking change to a method set or
	// input/output shape. Must be >= 1.
	Version int
	Methods []Method
}

// Method is one bridge endpoint. Handler receives the strict-decoded
// request body (a JSON object, possibly empty) and returns a value the
// chokepoint marshals into {"ok":true,"result":…}.
type Method struct {
	Name        string
	Description string
	// Input and Output are JSON Schema documents (or nil when the
	// method takes / returns nothing worth describing). They ship in
	// the manifest and drive the generated .d.ts.
	Input  json.RawMessage
	Output json.RawMessage
	// Permission is "" for ungated methods, else "resource:verb".
	// The chokepoint consults the grant store and (on a miss) the OS
	// prompt before the handler runs.
	Permission string
	Handler    func(ctx context.Context, in json.RawMessage) (any, error)
}

// Manifest is the frozen, deterministic description of everything the
// host serves. Served at /__gofastr/desktop/manifest.json and baked
// into the generated bridge script.
type Manifest struct {
	Schema       int              `json:"schema"`
	Host         HostInfo         `json:"host"`
	Capabilities []CapabilityInfo `json:"capabilities"`
}

// HostInfo names the host the manifest was frozen on; a capability
// registered on one OS only is simply absent on the others.
type HostInfo struct {
	OS      string `json:"os"`
	Arch    string `json:"arch"`
	Version string `json:"version"`
}

// CapabilityInfo is a capability without its handlers.
type CapabilityInfo struct {
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Version     int          `json:"version"`
	Methods     []MethodInfo `json:"methods"`
}

// MethodInfo is a method without its handler.
type MethodInfo struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Permission  string          `json:"permission,omitempty"`
	Input       json.RawMessage `json:"input,omitempty"`
	Output      json.RawMessage `json:"output,omitempty"`
}

// registered records one accepted capability plus where it came from,
// so a duplicate registration error can name the FIRST site.
type registered struct {
	cap    Capability
	origin string
}

// registry holds the capabilities until Run freezes it.
type registry struct {
	mu     sync.Mutex
	caps   map[string]registered
	frozen bool
}

func newRegistry() *registry {
	return &registry{caps: make(map[string]registered)}
}

// register validates and records cap. origin names the caller's file
// and line, recorded for duplicate-name errors.
func (r *registry) register(cap Capability, origin string) error {
	if err := validateCapability(cap); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.frozen {
		return fmt.Errorf("desktop: Register(%q) after the registry froze at Run; register capabilities from a plugin or battery Init", cap.Name)
	}
	if prev, ok := r.caps[cap.Name]; ok {
		return fmt.Errorf("desktop: capability %q is already registered (first registration at %s); this registration from %s must use a different name", cap.Name, prev.origin, origin)
	}
	r.caps[cap.Name] = registered{cap: cap, origin: origin}
	return nil
}

// lookup returns the capability by name.
func (r *registry) lookup(name string) (Capability, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rc, ok := r.caps[name]
	return rc.cap, ok
}

// freeze stops further registration and returns the frozen manifest
// basis.
func (r *registry) freeze() {
	r.mu.Lock()
	r.frozen = true
	r.mu.Unlock()
}

func (r *registry) isFrozen() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.frozen
}

// names returns the registered capability names, sorted.
func (r *registry) names() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Sorted(maps.Keys(r.caps))
}

// build renders the deterministic Manifest. host version comes from
// the build info when present.
func (r *registry) build() Manifest {
	r.mu.Lock()
	snapshot := make([]registered, 0, len(r.caps))
	for _, name := range slices.Sorted(maps.Keys(r.caps)) {
		snapshot = append(snapshot, r.caps[name])
	}
	r.mu.Unlock()

	m := Manifest{
		Schema:       1,
		Host:         HostInfo{OS: runtime.GOOS, Arch: runtime.GOARCH, Version: moduleVersion()},
		Capabilities: make([]CapabilityInfo, 0, len(snapshot)),
	}
	for _, rc := range snapshot {
		ci := CapabilityInfo{
			Name:        rc.cap.Name,
			Description: rc.cap.Description,
			Version:     rc.cap.Version,
			Methods:     make([]MethodInfo, 0, len(rc.cap.Methods)),
		}
		for _, mm := range rc.cap.Methods {
			ci.Methods = append(ci.Methods, MethodInfo{
				Name:        mm.Name,
				Description: mm.Description,
				Permission:  mm.Permission,
				Input:       compactJSON(mm.Input),
				Output:      compactJSON(mm.Output),
			})
		}
		slices.SortFunc(ci.Methods, func(a, b MethodInfo) int {
			if a.Name < b.Name {
				return -1
			}
			return 1
		})
		m.Capabilities = append(m.Capabilities, ci)
	}
	return m
}

// validateCapability enforces every grammar rule, naming the field
// that failed.
func validateCapability(cap Capability) error {
	if !reCapabilityName.MatchString(cap.Name) {
		return fmt.Errorf("desktop: capability name %q must match %s", cap.Name, reCapabilityName.String())
	}
	if cap.Version < 1 {
		return fmt.Errorf("desktop: capability %q: version must be >= 1, got %d", cap.Name, cap.Version)
	}
	if len(cap.Methods) == 0 {
		return fmt.Errorf("desktop: capability %q must declare at least one method", cap.Name)
	}
	seen := make(map[string]bool, len(cap.Methods))
	for _, m := range cap.Methods {
		if !reMethodName.MatchString(m.Name) {
			return fmt.Errorf("desktop: capability %q: method name %q must match %s", cap.Name, m.Name, reMethodName.String())
		}
		if seen[m.Name] {
			return fmt.Errorf("desktop: capability %q: duplicate method name %q", cap.Name, m.Name)
		}
		seen[m.Name] = true
		if m.Permission != "" && !rePermission.MatchString(m.Permission) {
			return fmt.Errorf("desktop: capability %q method %q: permission %q must be resource:verb (lowercase letters, digits, -, _; one colon)", cap.Name, m.Name, m.Permission)
		}
		if m.Handler == nil {
			return fmt.Errorf("desktop: capability %q method %q: Handler must not be nil", cap.Name, m.Name)
		}
		if len(m.Input) > 0 && !json.Valid(m.Input) {
			return fmt.Errorf("desktop: capability %q method %q: Input is not valid JSON", cap.Name, m.Name)
		}
		if len(m.Output) > 0 && !json.Valid(m.Output) {
			return fmt.Errorf("desktop: capability %q method %q: Output is not valid JSON", cap.Name, m.Name)
		}
	}
	return nil
}

// compactJSON normalizes a RawMessage so two registrations of the same
// schema text always marshal identically in the manifest.
func compactJSON(in json.RawMessage) json.RawMessage {
	if len(in) == 0 {
		return nil
	}
	var v any
	if err := json.Unmarshal(in, &v); err != nil {
		return in
	}
	out, err := json.Marshal(v)
	if err != nil {
		return in
	}
	return out
}

// moduleVersion returns the module's version string, or "dev" outside
// a versioned build.
func moduleVersion() string {
	if v := buildVersion(); v != "" {
		return v
	}
	return "dev"
}
