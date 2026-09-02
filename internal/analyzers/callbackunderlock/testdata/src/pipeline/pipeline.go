// Package pipeline is a NOVEL instantiation of the callback-under-lock
// shape: a staged processing pipeline (no such code exists in this
// repo) whose stages are func fields, invoked under a plain Mutex, with
// a direct map-element call and the clear-post-release spelling.
package pipeline

import (
	"context"
	"sync"
)

type Stage func(ctx context.Context, in []byte) ([]byte, error)

type Pipe struct {
	mu    sync.Mutex
	stage Stage
	hooks map[string]func()
}

// badStages calls the stage field under the lock.
func (p *Pipe) badStages(ctx context.Context, in []byte) {
	p.mu.Lock()
	out, err := p.stage(ctx, in) // want `callbackunderlock: p.stage is called while p.mu is held`
	p.mu.Unlock()
	_, _ = out, err
}

// badHook calls a map element directly under the lock.
func (p *Pipe) badHook(name string) {
	p.mu.Lock()
	p.hooks[name]() // want `callbackunderlock: p\.hooks\[name\] is called while p\.mu is held`
	p.mu.Unlock()
}

// goodStages reads the field under the lock and invokes it after
// release.
func (p *Pipe) goodStages(ctx context.Context, in []byte) {
	p.mu.Lock()
	stage := p.stage
	p.mu.Unlock()
	_, _ = stage(ctx, in)
}

// afterUnlock is quiet: the release precedes the call in source order
// on the same mutex.
func (p *Pipe) afterUnlock(ctx context.Context, in []byte) {
	p.mu.Lock()
	stage := p.stage
	p.mu.Unlock()
	_, _ = stage(ctx, in)
	p.hooks["done"]()
}
