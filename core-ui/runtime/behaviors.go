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

func init() {
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

var behaviorCache sync.Map

type servedBehavior struct {
	sourceHash string
	served     string
	hash       string
}

func behaviorModule(e *registry.BehaviorEntry) *servedBehavior {
	if v, ok := behaviorCache.Load(e.Name); ok {
		if sb := v.(*servedBehavior); sb.sourceHash == e.SourceHash() {
			return sb
		}
	}
	src := minify.ServedJS(e.Source)
	sum := sha256.Sum256([]byte(src))
	sb := &servedBehavior{sourceHash: e.SourceHash(), served: src, hash: hex.EncodeToString(sum[:8])}
	behaviorCache.Store(e.Name, sb)
	return sb
}

func embeddedModule(name string) (string, bool) {
	modulesOnce.Do(loadModules)
	src, ok := modulesData[name]
	return src, ok
}

func registeredModule(name string) (string, bool) {
	e, ok := registry.LookupBehavior(name)
	if !ok {
		return "", false
	}
	return behaviorModule(e).served, true
}

// ModuleHash returns a module's version, eight hex bytes of SHA-256 over its served bytes, or "".
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

type behaviorManifest struct {
	Selectors    []string               `json:"s"`
	Idle         bool                   `json:"i,omitempty"`
	Requires     []string               `json:"r,omitempty"`
	Interactions []registry.Interaction `json:"x,omitempty"`
}

// BehaviorsJSON returns the behaviours block the kernel reads, or nil when none is registered.
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
		if e.OnRequest {
			continue
		}
		out[e.Name] = behaviorManifest{Selectors: append([]string(nil), e.Markers...), Idle: e.Idle, Requires: append([]string(nil), e.Requires...), Interactions: append([]registry.Interaction(nil), e.Interactions...)}
	}
	validateRequirements(all)
	buf, err := json.Marshal(out)
	if err != nil {
		panic("runtime: the behaviours block cannot be marshalled: " + err.Error())
	}
	return buf
}

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
