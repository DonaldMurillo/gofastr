package engine

import (
	"context"
	"sync"
)

// CancelTree is a tree of cancellable contexts: cancelling a node cancels its descendants.
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

// Child returns a child node that the parent's cancellation reaches and whose own cancellation does not reach the parent or siblings.
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

// Cause returns the reason this node was cancelled, or nil.
func (t *CancelTree) Cause() error {
	return context.Cause(t.root)
}
