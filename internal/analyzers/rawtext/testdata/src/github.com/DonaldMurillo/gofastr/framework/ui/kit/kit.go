// Package kit pins ctxless's no-context arm (2026-10-28, U10 and U21):
// kit code building a child that always reads its Ctx, from a function
// no context reaches.
package kit

import (
	"context"

	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type CodeBlockConfig struct {
	Ctx  context.Context
	Code string
}

// CodeBlock reads Ctx on every call.
func CodeBlock(cfg CodeBlockConfig) string {
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	_ = ctx
	return cfg.Code
}

type ButtonConfig struct {
	Ctx      context.Context
	Shortcut string
}

// Button reads Ctx only for its shortcut hint.
func Button(cfg ButtonConfig) string {
	if cfg.Shortcut != "" {
		_ = cfg.Ctx
	}
	return ""
}

type MarkdownConfig struct{ Source string }

// Markdown is ui.Markdown (U10): no Ctx to forward.
func Markdown(cfg MarkdownConfig) string {
	return CodeBlock(CodeBlockConfig{Code: cfg.Source}) // want `CodeBlock gets no Ctx and no context reaches this function`
}

// tabs is ui.CodeTabs (U21): inside a loop still counts.
func tabs(codes []string) []string {
	var out []string
	for _, c := range codes {
		out = append(out, CodeBlock(CodeBlockConfig{Code: c})) // want `CodeBlock gets no Ctx`
	}
	return out
}

// forwarded passes a context it was given: the main arm's business.
func forwarded(ctx context.Context, code string) string {
	return CodeBlock(CodeBlockConfig{Ctx: ctx, Code: code})
}

type PageConfig struct {
	Ctx  context.Context
	Code string
}

// fromConfig reaches a context through its own config: not orphaned.
func fromConfig(cfg PageConfig) string {
	return CodeBlock(CodeBlockConfig{Ctx: cfg.Ctx, Code: cfg.Code})
}

// branchOnly builds a Button with no shortcut: Ctx is never read.
func branchOnly() string {
	return Button(ButtonConfig{})
}

// setsCtx sets Ctx explicitly even with no context in scope.
func setsCtx(code string) string {
	return CodeBlock(CodeBlockConfig{Ctx: context.TODO(), Code: code})
}

// otherPkg calls into another package: its body is out of reach.
func otherPkg() ui.TagConfig {
	return ui.TagConfig{}
}

// readsInLiteral reads Ctx only inside a closure.
func readsInLiteral(cfg CodeBlockConfig) func() context.Context {
	return func() context.Context { return cfg.Ctx }
}

// readsInLoop reads Ctx only inside a loop body.
func readsInLoop(cfg CodeBlockConfig) {
	for i := 0; i < len(cfg.Code); i++ {
		_ = cfg.Ctx
	}
}

// viaLoop builds the child through readsInLoop: not always read.
func viaLoop() {
	readsInLoop(CodeBlockConfig{})
}

var defaults PageConfig

// readsOther always reads a package value's Ctx, never its config's.
func readsOther(cfg CodeBlockConfig) context.Context {
	_ = cfg.Code
	return defaults.Ctx
}

// viaOther builds the child through readsOther.
func viaOther() context.Context {
	return readsOther(CodeBlockConfig{})
}

// inScope has a context: the main arm reports, not the orphan arm.
func inScope(ctx context.Context, code string) string {
	_ = ctx
	return CodeBlock(CodeBlockConfig{Code: code}) // want `kit.CodeBlockConfig leaves Ctx unset while a request context is in scope`
}

// viaClosure builds the child through readsInLiteral: not always read.
func viaClosure() func() context.Context {
	return readsInLiteral(CodeBlockConfig{})
}
