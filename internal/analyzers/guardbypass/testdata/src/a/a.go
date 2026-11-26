// Package a reduces framework/uihost: it imports lib's hooks and
// guards, and only lib's exported guards reach it, as facts.
package a

import (
	"context"
	"lib"
	"strings"
)

func partialHead(ctx context.Context, t lib.Titler, l lib.Langer) (string, string) {
	return t.Title(ctx), l.Lang() // want `Title is called directly, but lib.SafeTitle exists` `Lang is called directly, but lib.SafeLang exists`
}

// throughEmbed calls Title through Meta, which embeds Titler.
func throughEmbed(ctx context.Context, m lib.Meta) string {
	return m.Title(ctx) // want `call lib.SafeTitle`
}

func viaGuard(ctx context.Context, t lib.Titler, l lib.Langer) (string, string) {
	return lib.SafeTitle(ctx, t), lib.SafeLang(l)
}

func ownRecover(ctx context.Context, t lib.Titler) (title string) {
	defer func() {
		if recover() != nil {
			title = "fallback"
		}
	}()
	return strings.ToUpper(t.Title(ctx))
}

// syncLiteral runs the literal on the recovering frame.
func syncLiteral(ctx context.Context, t lib.Titler) (title string) {
	defer func() {
		if recover() != nil {
			title = ""
		}
	}()
	read := func() string { return t.Title(ctx) }
	return read()
}

// launched runs the call on its own goroutine: the recover above it
// never sees that panic.
func launched(ctx context.Context, t lib.Titler) {
	defer func() { _ = recover() }()
	go func() {
		_ = t.Title(ctx) // want `call lib.SafeTitle`
	}()
}

// beforeDefer calls the hook before the recover is in place.
func beforeDefer(ctx context.Context, t lib.Titler) string {
	s := t.Title(ctx) // want `call lib.SafeTitle`
	defer func() { _ = recover() }()
	return s
}

// lib's safeDescribe and rethrow are not guards here.
func describe(d lib.Describer, c lib.Closer) (string, error) {
	return d.Describe(), c.Close()
}

// OtherTitler declares its own Title: a different method object.
type OtherTitler interface {
	Title(ctx context.Context) string
}

func other(ctx context.Context, o OtherTitler) string {
	return o.Title(ctx)
}

type Pinger interface {
	Ping()
}

// safePing is a's own unexported guard; the call inside it is quiet.
func safePing(p Pinger) {
	defer func() { _ = recover() }()
	p.Ping()
}

func ping(p Pinger) {
	p.Ping() // want `Ping is called directly, but safePing exists to recover its panic: call safePing`
}

type concrete struct{}

func (concrete) Title(context.Context) string { return "" }

// A concrete receiver is not the interface hook.
func onConcrete(ctx context.Context, c concrete) string {
	return c.Title(ctx)
}

var _ = []any{partialHead, throughEmbed, viaGuard, ownRecover, syncLiteral, launched, beforeDefer, describe, other, safePing, ping, onConcrete}
