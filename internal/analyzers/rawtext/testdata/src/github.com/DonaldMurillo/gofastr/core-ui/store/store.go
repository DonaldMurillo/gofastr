// Package store fakes core-ui/store's Slice for the ctxless arm.
package store

import "context"

// Slice is a declared signal with a default and a per-request value.
type Slice[T any] struct {
	name string
	def  T
}

// Name returns the signal name.
func (sl *Slice[T]) Name() string { return sl.name }

// Default returns the declared default.
func (sl *Slice[T]) Default() T { return sl.def }

// Value returns the request's value: the seed on ctx, else the default.
func (sl *Slice[T]) Value(ctx context.Context) T { return sl.def }
