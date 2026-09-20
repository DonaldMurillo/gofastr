package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"sort"
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/runtime/minify"
)

// Registered behaviours (registry.RegisterBehavior) are runtime modules
// from the host down: Module serves them, ModuleNames lists them,
// ModuleHash versions them, and NeededModules preloads them. This file
// is where the registry's stored source becomes served bytes, under the
// same minification gate as the embedded modules, and where the kernel's
// scan list is told about their markers. See docs/spec-behavior-registry.md.

func init() {
	// The embedded modules' names are reserved before any component
	// package registers, so a registration that would shadow one panics
	// where it is written. init order guarantees it: this package's
	// init runs before any package that imports it, and every package
	// that registers a behaviour renders through the runtime.
	// The directory is embedded, so this read cannot fail in a built
	// binary; if it somehow does, the reservation is skipped and a
	// shadowing registration is caught later by the panic in
	// BehaviorsJSON instead of here. Losing the better error message is
	// worth more than a panic in an init nobody can act on.
	entries, err := fs.ReadDir(modulesFS, "src")
	if err != nil {
		return
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if n := e.Name(); strings.HasSuffix(n, ".js") {
			names = append(names, strings.TrimSuffix(n, ".js"))
		}
	}
	registry.ReserveBehaviorNames(names...)
}

// behaviorCache holds the served source and hash per registered
// behaviour, keyed by name and invalidated by the registration's source
// hash, so a test that re-registers a different source under one name
// is served the new one.
var behaviorCache sync.Map // name -> *servedBehavior

type servedBehavior struct {
	sourceHash string
	served     string
	hash       string
}

// behaviorModule returns the served source of a registered behaviour,
// minified under the gate the embedded modules use.
func behaviorModule(e *registry.BehaviorEntry) *servedBehavior {
	if v, ok := behaviorCache.Load(e.Name); ok {
		if sb := v.(*servedBehavior); sb.sourceHash == e.SourceHash() {
			return sb
		}
	}
	src := e.Source
	if !nominify() {
		src = minify.Minify(src)
	}
	sum := sha256.Sum256([]byte(src))
	sb := &servedBehavior{sourceHash: e.SourceHash(), served: src, hash: hex.EncodeToString(sum[:8])}
	behaviorCache.Store(e.Name, sb)
	return sb
}

// embeddedModule returns an embedded module's source, or "", false.
func embeddedModule(name string) (string, bool) {
	modulesOnce.Do(loadModules)
	src, ok := modulesData[name]
	return src, ok
}

// registeredModule returns a registered behaviour's served source, or
// "", false.
func registeredModule(name string) (string, bool) {
	e, ok := registry.LookupBehavior(name)
	if !ok {
		return "", false
	}
	return behaviorModule(e).served, true
}

// ModuleHash returns the content-addressed version of a module, eight
// hex bytes of SHA-256 over the served bytes: what the manifest carries
// and the ?v= the loader appends. Empty for an unknown name.
func ModuleHash(name string) string {
	if src, ok := embeddedModule(name); ok {
		embeddedHashesOnce.Do(func() {
			for _, n := range embeddedModuleNames() {
				if s, ok := embeddedModule(n); ok {
					sum := sha256.Sum256([]byte(s))
					embeddedHashes[n] = hex.EncodeToString(sum[:8])
				}
			}
		})
		_ = src
		return embeddedHashes[name]
	}
	if e, ok := registry.LookupBehavior(name); ok {
		return behaviorModule(e).hash
	}
	return ""
}

var (
	embeddedHashesOnce sync.Once
	embeddedHashes     = map[string]string{}
)

func embeddedModuleNames() []string {
	modulesOnce.Do(loadModules)
	out := make([]string, 0, len(modulesData))
	for name := range modulesData {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// behaviorManifest is the shape of one entry in the behaviours block:
// the markers the kernel scans for, whether the load defers to idle,
// the modules loaded before this one, and the interactions the
// kernel's bridge retains while the module is fetching. The
// interactions ride under "x" in the bridge's own spec shape
// (registry.Interaction's JSON tags), so the kernel hands the array
// to its install loop without translating it — the bytes land in the
// page's manifest, not the core bundle.
type behaviorManifest struct {
	Selectors    []string               `json:"s"`
	Idle         bool                   `json:"i,omitempty"`
	Requires     []string               `json:"r,omitempty"`
	Interactions []registry.Interaction `json:"x,omitempty"`
}

// BehaviorsJSON returns the behaviours block the kernel reads to learn
// registered markers, requirements and interactions:
// {"<name>": {"s": ["[data-x]"], "i": true, "r": ["dep"], "x": [...]}}.
// Nil when nothing is registered. Live pages receive it as
// window.__gofastr_behaviors from /__gofastr/manifest.js; exports and
// the embed frame as the inline block #gofastr-behaviors — every
// delivery path is this one function, so a field added here reaches
// each of them.
func BehaviorsJSON() []byte {
	all := registry.Behaviors()
	if len(all) == 0 {
		return nil
	}
	out := make(map[string]behaviorManifest, len(all))
	for _, e := range all {
		if _, shadowed := embeddedModule(e.Name); shadowed {
			panic("runtime: behaviour " + e.Name + " shadows an embedded runtime module of the same name")
		}
		out[e.Name] = behaviorManifest{Selectors: append([]string(nil), e.Markers...), Idle: e.Idle, Requires: append([]string(nil), e.Requires...), Interactions: append([]registry.Interaction(nil), e.Interactions...)}
	}
	validateRequirements(all)
	// Refused the way a shadowed name is, six lines above, and for the
	// same reason: both are programming errors in the registry, not
	// runtime conditions, and the descriptor types hold nothing
	// encoding/json cannot render. Returning nil instead would ship
	// every page with no behaviours block at all — every registered
	// behaviour dead, no marker scanned, no interaction retained — and
	// nothing in the page or the log would say why.
	buf, err := json.Marshal(out)
	if err != nil {
		panic("runtime: the behaviours block cannot be marshalled: " + err.Error())
	}
	return buf
}

// validateRequirements refuses a dependency graph the loader could
// never satisfy, here at manifest time so the failure names the
// culprit rather than leaving a page whose module waits forever. The
// check runs when the block is first built — the first render (or
// first /__gofastr/manifest.js request), not process startup: the
// registry is only complete once every package's init has run, and
// this is the first reader of the whole graph. On a host whose chain
// carries the framework's recovery middleware (framework.App wires
// core/middleware.RecoveryFn) the panic surfaces as a 500 carrying
// the cycle path in the log line. A requirement must be an embedded
// module (embedded modules declare no requirements of their own, so
// they are the graph's leaves) or another registered behaviour; a
// cycle among registered behaviours is refused with its path.
func validateRequirements(all []*registry.BehaviorEntry) {
	byName := make(map[string]*registry.BehaviorEntry, len(all))
	for _, e := range all {
		byName[e.Name] = e
	}
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make(map[string]int, len(all))
	var visit func(e *registry.BehaviorEntry, path []string)
	visit = func(e *registry.BehaviorEntry, path []string) {
		switch color[e.Name] {
		case gray:
			panic("runtime: requirement cycle: " + strings.Join(append(path, e.Name), " -> "))
		case black:
			return
		}
		color[e.Name] = gray
		for _, r := range e.Requires {
			req, ok := byName[r]
			if !ok {
				if _, embedded := embeddedModule(r); !embedded {
					panic("runtime: behaviour " + e.Name + " requires " + r + ", which is neither an embedded runtime module nor a registered behaviour")
				}
				continue
			}
			visit(req, append(path, e.Name))
		}
		color[e.Name] = black
	}
	for _, e := range all {
		visit(e, nil)
	}
}

// neededBehaviors returns the registered behaviours whose markers
// appear in pageHTML, matched the way the demand-load table's markers
// are: as a complete attribute name (or name="value") at an
// attribute-name boundary.
func neededBehaviors(pageHTML string) []string {
	var out []string
	for _, e := range registry.Behaviors() {
		for _, m := range e.Markers {
			if sub := registry.MarkerSubstring(m); sub != "" && markerPresent(pageHTML, sub) {
				out = append(out, e.Name)
				break
			}
		}
	}
	return out
}
