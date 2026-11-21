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

	"github.com/DonaldMurillo/gofastr/internal/jsonexact"
)

var (
	reCapabilityName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	reMethodName     = regexp.MustCompile(`^[a-z][A-Za-z0-9_]{0,63}$`)
	rePermission     = regexp.MustCompile(`^[a-z][a-z0-9_-]*:[a-z][a-z0-9_-]*$`)
)

var reservedCapabilityNames = map[string]bool{
	"available": true,
	"call":      true,
	"on":        true,
	"off":       true,
	"manifest":  true,
}

// Capability is one named group of bridge methods ("clipboard", "window", a plugin's own).
type Capability struct {
	Name        string
	Description string
	// Version is bumped on any breaking change to a method set or input/output shape.
	Version int
	Methods []Method
}

// Method is one bridge endpoint.
type Method struct {
	Name        string
	Description string
	// Input and Output are JSON Schema documents (or nil when the method takes / returns nothing worth describing).
	Input  json.RawMessage
	Output json.RawMessage
	// Permission is "" for ungated methods, else "resource:verb".
	Permission string
	Handler    func(ctx context.Context, in json.RawMessage) (any, error)
}

// Manifest is the frozen, deterministic description of everything the host serves.
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

type registered struct {
	cap    Capability
	origin string
}

type registry struct {
	mu     sync.Mutex
	caps   map[string]registered
	frozen bool
}

func newRegistry() *registry {
	return &registry{caps: make(map[string]registered)}
}

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

func (r *registry) lookup(name string) (Capability, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rc, ok := r.caps[name]
	return rc.cap, ok
}

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

func validateCapability(cap Capability) error {
	if !reCapabilityName.MatchString(cap.Name) {
		return fmt.Errorf("desktop: capability name %q must match %s", cap.Name, reCapabilityName.String())
	}
	if reservedCapabilityNames[cap.Name] {
		return fmt.Errorf("desktop: capability name %q is reserved by the runtime's desktop object", cap.Name)
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

func compactJSON(in json.RawMessage) json.RawMessage {
	if len(in) == 0 {
		return nil
	}
	var v any
	if err := jsonexact.Unmarshal(in, &v); err != nil {
		return in
	}
	out, err := json.Marshal(v)
	if err != nil {
		return in
	}
	return out
}

func moduleVersion() string {
	if v := buildVersion(); v != "" {
		return v
	}
	return "dev"
}
