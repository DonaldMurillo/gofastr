package engine

import (
	"context"
	"sync"
)

// CancelTree wires cancellation hierarchically so cancelling a turn
// propagates to its tool calls and to any child engines it spawned
// (e.g., via the `delegate` tool).
//
// The doc commits to: cancellation tree (turn → tool calls → child
// engines). v0.x scopes `delegate` to sync-only-blocking; the child
// engine has its own CancelTree rooted at the delegate's call.
type CancelTree struct {
	mu       sync.Mutex
	root     context.Context
	cancel   context.CancelCauseFunc
	children []*CancelTree
}

// NewCancelTree returns a root CancelTree bound to parent.
func NewCancelTree(parent context.Context) *CancelTree {
	ctx, cancel := context.WithCancelCause(parent)
	return &CancelTree{root: ctx, cancel: cancel}
}

// Context returns the context tied to this node.
func (t *CancelTree) Context() context.Context { return t.root }

// Child returns a child node. Cancelling the parent cancels every
// child (transitively). Cancelling a child does not affect the parent
// or siblings.
func (t *CancelTree) Child() *CancelTree {
	t.mu.Lock()
	defer t.mu.Unlock()
	ctx, cancel := context.WithCancelCause(t.root)
	c := &CancelTree{root: ctx, cancel: cancel}
	t.children = append(t.children, c)
	return c
}

// Cancel cancels this node and all descendants with the given cause.
func (t *CancelTree) Cancel(cause error) {
	t.cancel(cause)
}

// Cause returns the reason this node was cancelled (or nil if not).
func (t *CancelTree) Cause() error {
	return context.Cause(t.root)
}
