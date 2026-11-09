package releasetest

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/upgrade"
	"github.com/DonaldMurillo/gofastr/internal/upgrade/scantest"
)

// v097Note finds a v0.97.0 note by the start of its change text.
func v097Note(t *testing.T, prefix string) *upgrade.Note {
	t.Helper()
	return scantest.NoteByChange(t, scantest.Registry(t), "v0.97.0", prefix)
}

// v096AuthKit is the v0.96 battery/auth surface the shared-budget note
// reads: each mail plugin's config still carries AddressRateLimit.
var v096AuthKit = map[string]string{"battery/auth/auth.go": `package auth

import "time"

type RateLimiterConfig struct {
	MaxAttempts int
	Window      time.Duration
}

type MagicLinkConfig struct {
	BaseURL          string
	AddressRateLimit *RateLimiterConfig
}

type PasswordResetConfig struct {
	BaseURL          string
	AddressRateLimit *RateLimiterConfig
}

type MagicLinkPlugin struct{}
type PasswordResetPlugin struct{}

func NewMagicLinkPlugin(MagicLinkConfig) *MagicLinkPlugin             { return nil }
func NewPasswordResetPlugin(PasswordResetConfig) *PasswordResetPlugin { return nil }
`}

// The note names the two removed fields, so an app that set either is
// told to move it, and an app that never did (every plain
// NewMagicLinkPlugin call) hears nothing: the call itself still builds.
func TestV097MailBudgetNoteHitsRemovedFields(t *testing.T) {
	n := v097Note(t, "BREAKING - the AddressRateLimit fields")
	app := scantest.App(t, map[string]string{"main.go": `package main

import (
	"time"

	"github.com/DonaldMurillo/gofastr/battery/auth"
)

func main() {
	lim := &auth.RateLimiterConfig{MaxAttempts: 100, Window: time.Minute}
	_ = auth.NewMagicLinkPlugin(auth.MagicLinkConfig{BaseURL: "/", AddressRateLimit: lim})
	_ = auth.NewPasswordResetPlugin(auth.PasswordResetConfig{BaseURL: "/", AddressRateLimit: lim})
}
`}, scantest.Options{Kit: v096AuthKit})
	if got := scantest.Hits(scantest.Run(t, app, []*upgrade.Note{n}, upgrade.MarkerSinks{}), n); len(got) != 2 {
		t.Fatalf("hits = %v, want both AddressRateLimit keys", got)
	}
	quiet := scantest.App(t, map[string]string{"main.go": `package main

import "github.com/DonaldMurillo/gofastr/battery/auth"

func main() {
	_ = auth.NewMagicLinkPlugin(auth.MagicLinkConfig{BaseURL: "/"})
	_ = auth.NewPasswordResetPlugin(auth.PasswordResetConfig{BaseURL: "/"})
}
`}, scantest.Options{Kit: v096AuthKit})
	if got := scantest.Hits(scantest.Run(t, quiet, []*upgrade.Note{n}, upgrade.MarkerSinks{}), n); len(got) != 0 {
		t.Fatalf("fires on an app that never set AddressRateLimit: %v", got)
	}
}

// The MCP item-gate note points at every gate an app installs: a gate
// whose message the caller must read now wraps it with mcp.ShowRefusal,
// which is a decision per gate, so the note is review-tier.
func TestV097MCPGateNoteHitsGates(t *testing.T) {
	n := v097Note(t, "BREAKING - an MCP item gate's refusal")
	if !n.Review {
		t.Fatal("the gate options survive the release: the note must be hits: review")
	}
	kit := map[string]string{"core/mcp/mcp.go": `package mcp

import "context"

type ToolOption func()
type ResourceOption func()
type PromptOption func()
type ToolHandler func(ctx context.Context, params map[string]any) (any, error)

func WithToolGate(gate func(ctx context.Context) error) ToolOption         { return nil }
func WithResourceGate(gate func(ctx context.Context) error) ResourceOption { return nil }
func WithPromptGate(gate func(ctx context.Context) error) PromptOption     { return nil }
func Gated(gate func(ctx context.Context) error, h ToolHandler) ToolHandler { return h }
`}
	app := scantest.App(t, map[string]string{"main.go": `package main

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core/mcp"
)

func gate(context.Context) error { return nil }

func main() {
	_ = mcp.WithToolGate(gate)
	_ = mcp.WithResourceGate(gate)
	_ = mcp.WithPromptGate(gate)
	_ = mcp.Gated(gate, nil)
}
`}, scantest.Options{Kit: kit})
	if got := scantest.Hits(scantest.Run(t, app, []*upgrade.Note{n}, upgrade.MarkerSinks{}), n); len(got) != 4 {
		t.Fatalf("hits = %v, want the four gates", got)
	}
}

// The well-known origin note reaches every app through framework.NewApp,
// the call where AppConfig.TrustedProxies or BaseURL goes.
func TestV097WellKnownNoteHitsNewApp(t *testing.T) {
	n := v097Note(t, "BREAKING - X-Forwarded-Proto no longer changes")
	kit := map[string]string{"framework/app.go": `package framework

type AppConfig struct{ Name string }
type App struct{}

func NewApp(AppConfig) *App { return nil }
`}
	app := scantest.App(t, map[string]string{"main.go": `package main

import "github.com/DonaldMurillo/gofastr/framework"

func main() { _ = framework.NewApp(framework.AppConfig{Name: "x"}) }
`}, scantest.Options{Kit: kit})
	if got := scantest.Hits(scantest.Run(t, app, []*upgrade.Note{n}, upgrade.MarkerSinks{}), n); len(got) != 1 {
		t.Fatalf("hits = %v, want the NewApp call", got)
	}
}

// sdk.SchemaHash gained its jsonCase parameter: a call with the old
// one-argument shape is the edit, and a ported call is quiet.
func TestV097SchemaHashNoteHitsOldShape(t *testing.T) {
	n := v097Note(t, "BREAKING - sdk.SchemaHash takes the JSON case")
	kit := map[string]string{"framework/sdk/sdk.go": `package sdk

type NamedConfig struct{ Name string }

func SchemaHash(named []NamedConfig) string { return "" }
`}
	app := scantest.App(t, map[string]string{"main.go": `package main

import "github.com/DonaldMurillo/gofastr/framework/sdk"

func main() { _ = sdk.SchemaHash(nil) }
`}, scantest.Options{Kit: kit})
	if got := scantest.Hits(scantest.Run(t, app, []*upgrade.Note{n}, upgrade.MarkerSinks{}), n); len(got) != 1 {
		t.Fatalf("hits = %v, want the old-shape SchemaHash call", got)
	}
	ported := map[string]string{"framework/sdk/sdk.go": `package sdk

type NamedConfig struct{ Name string }

func SchemaHash(named []NamedConfig, jsonCase string) string { return "" }
`}
	quiet := scantest.App(t, map[string]string{"main.go": `package main

import "github.com/DonaldMurillo/gofastr/framework/sdk"

func main() { _ = sdk.SchemaHash(nil, "camelCase") }
`}, scantest.Options{Kit: ported})
	if got := scantest.Hits(scantest.Run(t, quiet, []*upgrade.Note{n}, upgrade.MarkerSinks{}), n); len(got) != 0 {
		t.Fatalf("fires on a ported call: %v", got)
	}
}
