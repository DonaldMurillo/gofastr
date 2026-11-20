package registry

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// BehaviorEntry is one registered behaviour.
type BehaviorEntry struct {
	// Name is the module name, served at /__gofastr/runtime/<Name>.js.
	Name string
	// Source is the JavaScript as registered.
	Source string
	// Markers are the attribute selectors whose presence loads the module.
	Markers []string
	// Idle defers the load to idle time after first paint.
	Idle bool
	// Requires are the modules loaded before this one, deduplicated in order.
	Requires []string
	// Interactions are what the kernel's bridge retains while the module fetches.
	Interactions []Interaction
	// OnRequest marks an API module that loads only through loadModule or Requires.
	OnRequest bool

	sourceHash string
}

// Interaction is one click or keydown the bridge retains and replays; the JSON
// tags are the wire shape the kernel reads.
type Interaction struct {
	Event    string   `json:"event"`
	Selector string   `json:"selector,omitempty"`
	Keys     []string `json:"keys,omitempty"`
	Scope    string   `json:"scope,omitempty"`
}

// SourceHash is the SHA-256 of the registered source, eight bytes as hex.
func (e *BehaviorEntry) SourceHash() string { return e.sourceHash }

// BehaviorOption configures a BehaviorEntry at registration time.
type BehaviorOption func(*BehaviorEntry)

// Markers declares the selectors, each [data-x] or [data-x="v"], that load the module.
func Markers(selectors ...string) BehaviorOption {
	return func(e *BehaviorEntry) { e.Markers = append(e.Markers, selectors...) }
}

// OnRequest registers an API module, loaded only by loadModule or another behaviour's Requires.
func OnRequest() BehaviorOption { return func(e *BehaviorEntry) { e.OnRequest = true } }

// LoadIdle defers the load to idle time after first paint.
func LoadIdle() BehaviorOption { return func(e *BehaviorEntry) { e.Idle = true } }

// Requires names the modules that must be loaded and registered before this one.
func Requires(names ...string) BehaviorOption {
	return func(e *BehaviorEntry) { e.Requires = append(e.Requires, names...) }
}

// Interactions declares the clicks and keydowns the kernel retains while the module fetches.
func Interactions(specs ...Interaction) BehaviorOption {
	return func(e *BehaviorEntry) {
		for _, spec := range specs {
			spec.Keys = slices.Clone(spec.Keys)
			e.Interactions = append(e.Interactions, spec)
		}
	}
}

// Behavior is the handle RegisterBehavior returns.
type Behavior struct{ e *BehaviorEntry }

// Name returns the registered name.
func (b *Behavior) Name() string { return b.e.Name }

// Entry returns the underlying entry.
func (b *Behavior) Entry() *BehaviorEntry { return b.e }

var (
	behaviors             = map[string]*BehaviorEntry{}
	behaviorMarkerAttrs   = map[string]bool{}
	reservedBehaviorNames = map[string]bool{}
)

var (
	behaviorName   = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)
	behaviorMarker = regexp.MustCompile(`^\[(data-[a-z0-9-]+)(="[^"\]\\\x00-\x1f\x7f]*")?\]$`)
)

var (
	iaAttr     = `\[(?:[A-Za-z][A-Za-z0-9_-]*)(?:="[^"\\\x00-\x1f\x7f]*")?\]`
	iaWS       = `[ \t\n\f\r]`
	iaCompound = `(?:` + iaAttr + `|:not\(` + iaAttr + `\))+`
	iaSelector = regexp.MustCompile(`^` + iaWS + `*` + iaCompound + `(?:` + iaWS + `*(?:` + iaWS + `+|[>+~]` + iaWS + `*)` + iaCompound + `)*` + iaWS + `*(?:,` + iaWS + `*` + iaCompound + `(?:` + iaWS + `*(?:` + iaWS + `+|[>+~]` + iaWS + `*)` + iaCompound + `)*` + iaWS + `*)*$`)
	iaEvent    = map[string]bool{"click": true, "keydown": true}
)

// ReserveBehaviorNames records the embedded runtime modules' names, which no behaviour may take.
func ReserveBehaviorNames(names ...string) {
	mu.Lock()
	defer mu.Unlock()
	for _, n := range names {
		reservedBehaviorNames[n] = true
		if _, taken := behaviors[n]; taken {
			panic("registry.ReserveBehaviorNames: behaviour " + n + " is already registered under an embedded runtime module's name — the two share one URL and one manifest")
		}
	}
}

// RegisterBehavior registers a runtime module under a unique name; every mistake panics.
func RegisterBehavior(name, js string, opts ...BehaviorOption) *Behavior {
	if !behaviorName.MatchString(name) {
		panic(fmt.Sprintf("registry.RegisterBehavior: name %q must match ^[a-z][a-z0-9-]{0,63}$ — it is a URL segment and a manifest key", name))
	}
	if strings.TrimSpace(js) == "" {
		panic("registry.RegisterBehavior(" + name + "): the source is empty — a module that binds nothing")
	}
	e := &BehaviorEntry{Name: name, Source: js}
	for _, o := range opts {
		o(e)
	}
	if e.OnRequest {
		if len(e.Markers) > 0 || e.Idle || len(e.Interactions) > 0 || len(e.Requires) > 0 {
			panic("registry.RegisterBehavior(" + name + "): OnRequest takes no Markers, LoadIdle, Interactions or Requires — an on-request module loads only when page code asks for it")
		}
	} else if len(e.Markers) == 0 {
		panic("registry.RegisterBehavior(" + name + "): no Markers — the kernel loads a behaviour when a marker appears, and this one would never load (an API module loaded by page code is OnRequest)")
	}
	for _, m := range e.Markers {
		if !behaviorMarker.MatchString(m) {
			panic(fmt.Sprintf("registry.RegisterBehavior(%s): marker %q must be an attribute selector on a data- attribute, [data-x] or [data-x=\"v\"]", name, m))
		}
	}
	for i, ia := range e.Interactions {
		where := fmt.Sprintf("registry.RegisterBehavior(%s): interaction %d", name, i)
		switch ia.Event {
		case "click":
			if ia.Keys != nil {
				panic(where + " (click): Keys is set — the click resolution never reads keys; declare them on a keydown spec")
			}
			if ia.Scope != "" {
				panic(where + " (click): Scope is set — the click resolution matches the node by Selector, not by a scope")
			}
			if ia.Selector == "" {
				panic(where + " (click): Selector is empty — the bridge would resolve no node and retain nothing")
			}
			if !iaSelector.MatchString(ia.Selector) {
				panic(fmt.Sprintf("%s (click): Selector %q must be a selector list of attribute selectors ([attr], [attr=\"v\"]), :not([attr]) and combinators — the grammar querySelector accepts, wider than a marker's", where, ia.Selector))
			}
		case "keydown":
			if ia.Selector != "" {
				panic(where + " (keydown): Selector is set — the keydown resolution matches by Keys and Scope, not by a node selector")
			}
			if len(ia.Keys) == 0 {
				panic(where + " (keydown): Keys is empty — the bridge retains no key, so the spec would install a listener that ignores every keypress")
			}
			for _, k := range ia.Keys {
				if k == "" {
					panic(where + " (keydown): Keys contains an empty key — e.key is never the empty string, the entry matches nothing")
				}
			}
			if ia.Scope == "" {
				panic(where + " (keydown): Scope is empty — the kernel resolves querySelector(scope) on every keydown and querySelector('') throws, so the listener would die on its first event")
			}
			if !iaSelector.MatchString(ia.Scope) {
				panic(fmt.Sprintf("%s (keydown): Scope %q must be a selector list of attribute selectors ([attr], [attr=\"v\"]), :not([attr]) and combinators — the grammar querySelector accepts, wider than a marker's", where, ia.Scope))
			}
		default:
			panic(fmt.Sprintf("%s: Event %q is not one the bridge retains — its node resolution branches on exactly click and keydown", where, ia.Event))
		}
	}
	deduped := e.Requires[:0]
	for _, r := range e.Requires {
		if r == name {
			panic("registry.RegisterBehavior(" + name + "): a behaviour cannot require itself — the loader would wait on its own registration")
		}
		if !behaviorName.MatchString(r) {
			panic(fmt.Sprintf("registry.RegisterBehavior(%s): requirement %q must match ^[a-z][a-z0-9-]{0,63}$ — it names a runtime module", name, r))
		}
		if !slices.Contains(deduped, r) {
			deduped = append(deduped, r)
		}
	}
	e.Requires = deduped
	sum := sha256.Sum256([]byte(js))
	e.sourceHash = hex.EncodeToString(sum[:8])

	mu.Lock()
	defer mu.Unlock()
	if reservedBehaviorNames[name] {
		panic("registry.RegisterBehavior(" + name + "): the name belongs to an embedded runtime module — the two share one URL and one manifest")
	}
	if existing, ok := behaviors[name]; ok {
		if !sameBehavior(existing, e) {
			panic(fmt.Sprintf("registry.RegisterBehavior: duplicate name %q with a different definition. Pick a unique name in one of the two call sites\n"+
				"  existing: source=%s markers=%v idle=%v requires=%v interactions=%v\n"+
				"  new:      source=%s markers=%v idle=%v requires=%v interactions=%v",
				name, existing.sourceHash, existing.Markers, existing.Idle, existing.Requires, existing.Interactions, e.sourceHash, e.Markers, e.Idle, e.Requires, e.Interactions))
		}
		return &Behavior{e: existing}
	}
	if frozen {
		panic("registry.RegisterBehavior(" + name + "): registered after the host built its manifest; register behaviours at package init")
	}
	behaviors[name] = e
	for _, m := range e.Markers {
		behaviorMarkerAttrs[behaviorMarker.FindStringSubmatch(m)[1]] = true
	}
	return &Behavior{e: e}
}

// IsMarkerAttr reports whether name is the attribute of a registered marker.
func IsMarkerAttr(name string) bool {
	mu.Lock()
	defer mu.Unlock()
	return behaviorMarkerAttrs[name]
}

// LookupBehavior returns the behaviour registered under name.
func LookupBehavior(name string) (*BehaviorEntry, bool) {
	mu.Lock()
	defer mu.Unlock()
	e, ok := behaviors[name]
	return e, ok
}

// Behaviors returns every registered behaviour, sorted by name.
func Behaviors() []*BehaviorEntry {
	mu.Lock()
	defer mu.Unlock()
	return slices.SortedFunc(maps.Values(behaviors), func(a, b *BehaviorEntry) int {
		return strings.Compare(a.Name, b.Name)
	})
}

// MarkerSubstring returns a marker selector as it appears in rendered HTML.
func MarkerSubstring(selector string) string {
	m := behaviorMarker.FindStringSubmatch(selector)
	if m == nil {
		return ""
	}
	if m[2] == "" {
		return m[1]
	}
	value := m[2][2 : len(m[2])-1]
	return m[1] + `="` + render.Escape(value) + `"`
}

func sameBehavior(a, b *BehaviorEntry) bool {
	return a.Name == b.Name && a.sourceHash == b.sourceHash && a.Idle == b.Idle && a.OnRequest == b.OnRequest && slices.Equal(a.Markers, b.Markers) && slices.Equal(a.Requires, b.Requires) && sameInteractions(a.Interactions, b.Interactions)
}

func sameInteractions(a, b []Interaction) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Event != b[i].Event || a[i].Selector != b[i].Selector || a[i].Scope != b[i].Scope || !slices.Equal(a[i].Keys, b[i].Keys) {
			return false
		}
	}
	return true
}
