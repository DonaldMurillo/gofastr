// Package app is the analysistest stand-in for
// github.com/DonaldMurillo/gofastr/core-ui/app, reduced to the
// symbols the layoutfunc lint keys on. The fixture run sets the
// analyzer's -module flag to example.app, so this package's import
// path (example.app/core-ui/app) is the one the analyzer treats as
// the real app package.
package app

import (
	"context"
	"net/http"
	"net/url"
)

// HTML stands in for render.HTML.
type HTML string

// Component stands in for component.Component.
type Component interface {
	Render() HTML
}

// Match is the route match snapshot (the real type lives in
// core-ui/app/match.go). Path and Param are the reads the lint flags.
type Match struct {
	path   string
	params map[string]string
}

func (m Match) Path() string             { return m.path }
func (m Match) Param(name string) string { return m.params[name] }

func MatchFromContext(ctx context.Context) (Match, bool) {
	return Match{}, false
}

func RequestFromContext(ctx context.Context) *http.Request {
	return nil
}

// LayoutTree addresses the layer's placement points.
type LayoutTree struct{}

func (l *LayoutTree) Primary() HTML           { return "" }
func (l *LayoutTree) Outlet(name string) HTML { return "" }
func (l *LayoutTree) RouteArea(name string, fn func(ctx context.Context, m Match) HTML) HTML {
	return ""
}

// LayoutSpec, OutletSpec, AreaSpec, Layout and NewLayout are the
// tree-layout declaration surface.
type LayoutSpec struct {
	Outlets []OutletSpec
	Areas   []AreaSpec
}

type OutletSpec struct{ Name string }

type AreaSpec struct{ Name string }

type Layout struct{}

// LayoutFunc is the build function type: what a flagged read is
// lexically inside.
type LayoutFunc func(ctx context.Context, l *LayoutTree) HTML

func NewLayout(name string, spec LayoutSpec, build LayoutFunc) *Layout {
	return &Layout{}
}

// QueryFromContext stands in for the request's URL query read.
func QueryFromContext(ctx context.Context) url.Values { return url.Values{} }

// Key, Resolver, NewKey, From and Get stand in for the route resolver
// surface (core-ui/app/resolver.go).
type Key[T any] struct{ name string }

type Resolver struct{}

func NewKey[T any](name string) *Key[T] { return &Key[T]{name: name} }

func (k *Key[T]) From(fn func(ctx context.Context) (T, error)) Resolver { return Resolver{} }

func (k *Key[T]) Get(ctx context.Context) (T, error) {
	var zero T
	return zero, nil
}
