// Package agentsinv is the process-wide inventory of agents.md snippets.
package agentsinv

import (
	"sort"
	"strings"
	"sync"
)

// Kind discriminates framework-root helpers from individual batteries.
type Kind string

const (
	// KindFramework is the framework root and its subpackages.
	KindFramework Kind = "framework"
	// KindBattery is a single battery/* package.
	KindBattery Kind = "battery"
)

// Entry describes a single agent-onboarding snippet.
type Entry struct {
	// Name is a battery's directory name or a framework sub-area's name.
	Name string
	// Kind is "framework" or "battery".
	Kind Kind
	// ImportPath is the import path of the package that registered it.
	ImportPath string
	// Markdown is the package's embedded agents.md; empty is a mistake.
	Markdown string
}

var (
	mu      sync.RWMutex
	entries []Entry
	missing []Entry
)

// Register adds e, or files it under [MissingMarkdown] when Markdown is empty.
func Register(e Entry) {
	mu.Lock()
	defer mu.Unlock()
	if strings.TrimSpace(e.Markdown) == "" {
		missing = append(missing, e)
		return
	}
	entries = append(entries, e)
}

// MissingMarkdown returns a copy of the entries registered with empty Markdown.
func MissingMarkdown() []Entry {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Entry, len(missing))
	copy(out, missing)
	return out
}

// All returns a copy of the inventory, framework entries first, by Name.
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

// TestingT is satisfied by *testing.T; [Reset] takes one.
type TestingT interface{ Helper() }

// Reset clears the inventory, for tests.
func Reset(_ TestingT) {
	mu.Lock()
	entries = entries[:0]
	missing = missing[:0]
	mu.Unlock()
}
