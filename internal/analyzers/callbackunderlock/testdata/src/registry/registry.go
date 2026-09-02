// Package registry mirrors core/mcp's listing shape: a per-caller gate
// (a func field on each registry entry) evaluated inside the registry
// read lock. badList is the pre-fix listTools/handlePromptsList/
// handleResourcesTemplatesList shape; goodList is the snapshot fix.
package registry

import (
	"context"
	"slices"
	"strings"
	"sync"
)

type Entry struct {
	Name string
	Gate func(ctx context.Context) error // the callback that must leave the lock
}

type Reg struct {
	mu      sync.RWMutex
	entries map[string]Entry

	// frameworkGate is a non-app callback the repo deliberately runs
	// under the lock through a local: see localSnapshotGate.
	frameworkGate func(name string) error
}

// badList evaluates the gate while holding the read lock.
func (r *Reg) badList(ctx context.Context) []Entry {
	r.mu.RLock()
	out := make([]Entry, 0, len(r.entries))
	for _, e := range r.entries {
		if e.Gate != nil && e.Gate(ctx) != nil { // want `callbackunderlock: e.Gate is called while r.mu is held`
			continue
		}
		out = append(out, e)
	}
	r.mu.RUnlock()
	slices.SortFunc(out, func(a, b Entry) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// badDeferList is the same shape with the release deferred: the lock is
// in force to the end of the body, which is still a held region.
func (r *Reg) badDeferList(ctx context.Context) []Entry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Entry, 0, len(r.entries))
	for _, e := range r.entries {
		if e.Gate(ctx) != nil { // want `callbackunderlock: e.Gate is called while r.mu is held`
			continue
		}
		out = append(out, e)
	}
	return out
}

// goodList is the fixed spelling: snapshot under the lock, evaluate
// the gates outside it. The named helper call is silent by design.
func (r *Reg) goodList(ctx context.Context) []Entry {
	r.mu.RLock()
	snapshot := make([]Entry, 0, len(r.entries))
	for _, e := range r.entries {
		snapshot = append(snapshot, e)
	}
	r.mu.RUnlock()
	out := make([]Entry, 0, len(snapshot))
	for _, e := range snapshot {
		if gateRefused(e.Gate, ctx) {
			continue
		}
		out = append(out, e)
	}
	return out
}

func gateRefused(gate func(ctx context.Context) error, ctx context.Context) bool {
	if gate == nil {
		return false
	}
	defer func() { recover() }()
	return gate(ctx) != nil
}

// localSnapshotGate pins the identifier silence: the repo's own
// convention copies a func field to a local under the lock and calls
// the local — core/mcp listTools' callGate — and this rule stays out,
// because "which callbacks the framework allows to block its registry"
// is not visible from shape.
func (r *Reg) localSnapshotGate(ctx context.Context) []Entry {
	r.mu.RLock()
	gate := r.frameworkGate
	out := make([]Entry, 0, len(r.entries))
	for _, e := range r.entries {
		if gate != nil && gate(e.Name) != nil {
			continue
		}
		out = append(out, e)
	}
	r.mu.RUnlock()
	return out
}
