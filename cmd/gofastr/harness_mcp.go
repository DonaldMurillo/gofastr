package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	xharness "github.com/DonaldMurillo/gofastr/framework/harness"
	"github.com/DonaldMurillo/gofastr/framework/harness/control/mcpserver"
	"github.com/DonaldMurillo/gofastr/framework/harness/secrets"
	"github.com/DonaldMurillo/gofastr/framework/harness/logging"
)

// runHarnessMCP is the `gofastr harness mcp` subcommand entry. It
// boots a minimal harness (no TUI, no web), creates one session, and
// hands stdio to the MCP server. Exits when stdin closes.
//
// Wire this into Claude Code / Codex / Cursor's MCP config:
//
//	{
//	  "mcpServers": {
//	    "gofastr-harness": {
//	      "command": "gofastr",
//	      "args": ["harness", "mcp", "--profile", "/path/to/default.toml"],
//	      "env": {
//	        "GOFASTR_HARNESS_PASSPHRASE": "your-passphrase",
//	        "ZAI_API_KEY": "your-zai-key"
//	      }
//	    }
//	  }
//	}
func runHarnessMCP(args []string) {
	fs := flag.NewFlagSet("harness mcp", flag.ExitOnError)
	useFramework := fs.Bool("framework", false, "use the framework preset profile")
	profilePath := fs.String("profile", "", "explicit profile TOML path")
	requiredToken := fs.String("required-token", "", "if set, GOFASTR_HARNESS_TOKEN env must match")
	_ = fs.Parse(args)

	// Load repo-local secrets (gitignored) before reading any env vars.
	// Shell env still wins — this is the fallback for cases where the
	// MCP client (Claude Code, Codex) doesn't pass env through.
	_, _ = secrets.LoadRepo()

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		osExit(1)
	}
	xdgConfig := filepath.Join(home, ".config", "gofastr", "harness")
	xdgState := filepath.Join(home, ".local", "share", "gofastr", "harness")
	_ = os.MkdirAll(xdgConfig, 0o700)
	_ = os.MkdirAll(xdgState, 0o700)

	prof, err := loadProfile(*useFramework, *profilePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		osExit(1)
	}

	// Stdio MCP needs stderr-only logging so JSON-RPC on stdout
	// stays clean.
	logger := logging.New(os.Stderr, logging.LevelWarn)

	cfg := xharness.Config{
		Profile:       prof,
		WorkingDir:    mustGetwd(),
		XDGConfig:     xdgConfig,
		XDGState:      xdgState,
		Logger:        logger,
		CredstorePass: os.Getenv("GOFASTR_HARNESS_PASSPHRASE"),
	}
	if cfg.CredstorePass == "" {
		cfg.CredstorePass = "harness-default-passphrase-change-me"
	}
	h, err := xharness.New(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		osExit(1)
	}
	defer h.Shutdown()

	// Create one default session bound to the profile's default model.
	prov, modelID := resolveDefaultModel(h, prof)
	if prov == nil {
		fmt.Fprintf(os.Stderr, "harness mcp: no provider matches %q\n", prof.DefaultModel)
		osExit(1)
	}
	_ = h.CreateSession(prov, modelID)

	// Boot the MCP server.
	srv := mcpserver.New(h.Mux, h.Catalog)
	srv.RequiredToken = *requiredToken
	if err := srv.Serve(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		osExit(1)
	}
}
