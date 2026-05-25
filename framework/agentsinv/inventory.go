// Package agentsinv is a process-wide registry of agent-onboarding
// snippets contributed by batteries and the framework root.
//
// Each contributing package embeds its own `agents.md`, then registers
// it from an init() so that:
//
//   - importing the battery == including the snippet in the generated
//     AGENTS.md (no central allow-list to maintain),
//   - the generator in cmd/gofastr can dump the inventory by importing
//     blank (`_ "...battery/admin"`) the packages it wants in scope.
//
// The contract is intentionally tiny — just enough to drive AGENTS.md
// generation. Per-battery prose lives in the package's own `agents.md`,
// not in this registry.
package agentsinv

import (
	"sort"
	"sync"
	"testing"
)

// Kind discriminates framework-root helpers from individual batteries.
type Kind string

const (
	// KindFramework is the framework root + its subpackages (audit,
	// dev/livereload, dotenv, etc.).
	KindFramework Kind = "framework"
	// KindBattery is a single battery/* package.
	KindBattery Kind = "battery"
)

// Entry describes a single agent-onboarding snippet.
type Entry struct {
	// Name identifies the snippet. For batteries: the directory name
	// (e.g. "admin"). For framework: a sub-area name (e.g. "auditlog").
	Name string
	// Kind is "framework" or "battery".
	Kind Kind
	// ImportPath is the canonical Go import path of the package that
	// registered the entry.
	ImportPath string
	// Markdown is the embedded contents of the package's agents.md.
	// MUST be non-empty — a missing file would silently drop the
	// entry from generated AGENTS.md.
	Markdown string
}

var (
	mu      sync.RWMutex
	entries []Entry
	missing []Entry // entries that registered with empty Markdown
)

// Register adds e to the inventory. Safe to call from init().
//
// If Markdown is empty (missing or stale `//go:embed agents.md`), the
// entry is recorded in MissingMarkdown() but NOT added to All(). This
// is a soft warning by design — a panic from package init would kill
// the entire gofastr binary for every subcommand (build / dev /
// migrate / version), and one mis-shipped battery shouldn't prevent
// users from running unrelated commands. The `gofastr agents` family
// surfaces the warning where it's actionable.
func Register(e Entry) {
	mu.Lock()
	defer mu.Unlock()
	if e.Markdown == "" {
		missing = append(missing, e)
		return
	}
	entries = append(entries, e)
}

// MissingMarkdown returns entries that registered with empty Markdown.
// Callers of `gofastr agents init/sync` print these as warnings so a
// developer who forgot to embed the file fixes it before the doc
// generator silently ships an incomplete AGENTS.md.
func MissingMarkdown() []Entry {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Entry, len(missing))
	copy(out, missing)
	return out
}

// All returns a sorted copy of the inventory. Framework entries come
// first (so the generated AGENTS.md leads with framework primitives
// before battery-specific sections), then batteries; alphabetical by
// Name within each kind.
func All() []Entry {
	mu.RLock()
	out := make([]Entry, len(entries))
	copy(out, entries)
	mu.RUnlock()
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind == KindFramework
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// Reset clears the registry. The *testing.T parameter is a discipline
// marker — production code can technically pass nil since `testing`
// is in stdlib, so this is convention, not enforcement.
func Reset(_ *testing.T) {
	mu.Lock()
	entries = entries[:0]
	missing = missing[:0]
	mu.Unlock()
}
