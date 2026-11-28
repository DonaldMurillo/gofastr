package harnesscmd

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	xharness "github.com/DonaldMurillo/gofastr/framework/experimental/harness"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control/mcpserver"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/logging"
)

// runHarnessMCP boots a harness with one session and serves it as a stdio
// MCP server until stdin closes.
func runHarnessMCP(args []string) {
	fs := flag.NewFlagSet("harness mcp", flag.ExitOnError)
	useFramework := fs.Bool("framework", false, "use the framework preset profile")
	profilePath := fs.String("profile", "", "explicit profile TOML path")
	requiredToken := fs.String("required-token", "", "if set, GOFASTR_HARNESS_TOKEN env must match")
	allowRepoSecrets := fs.Bool("allow-repo-secrets", false, "use provider keys from this repository's .harness-secrets/env without a remembered approval")
	_ = fs.Parse(args)

	xdgConfig, xdgState, err := harnessDirs()
	if err != nil {
		cli.Fprintln(os.Stderr, err)
		cli.OsExit(1)
		return
	}

	loadRepoSecrets(xdgConfig, *allowRepoSecrets, false, nil, os.Stderr)

	prof, err := loadProfile(*useFramework, *profilePath)
	if err != nil {
		cli.Fprintln(os.Stderr, err)
		cli.OsExit(1)
	}

	logger := logging.New(os.Stderr, logging.LevelWarn)

	machineKey, err := machineKeyFromEnv()
	if err != nil {
		cli.Fprintln(os.Stderr, err)
		cli.OsExit(1)
		return
	}
	cfg := xharness.Config{
		Profile:       prof,
		WorkingDir:    mustGetwd(),
		XDGConfig:     xdgConfig,
		XDGState:      xdgState,
		Logger:        logger,
		CredstorePass: os.Getenv("GOFASTR_HARNESS_PASSPHRASE"),
		MachineKey:    machineKey,
	}
	if cfg.CredstorePass == "" && len(cfg.MachineKey) == 0 {
		fmt.Fprintf(os.Stderr, "gofastr harness mcp: %s\n", noCredstoreKeyHelp)
		cli.OsExit(1)
		return
	}
	h, err := xharness.New(cfg)
	if err != nil {
		cli.Fprintln(os.Stderr, err)
		cli.OsExit(1)
	}
	defer h.Shutdown()

	prov, modelID := resolveDefaultModel(h, prof)
	if prov == nil {
		cli.Fprintf(os.Stderr, "harness mcp: no provider matches %q\n", prof.DefaultModel)
		cli.OsExit(1)
	}
	if _, err := h.CreateSession(prov, modelID); err != nil {
		cli.Fprintln(os.Stderr, err)
		cli.OsExit(1)
	}

	srv := mcpserver.New(h.Mux, h.Catalog)
	srv.RequiredToken = *requiredToken
	if err := srv.Serve(context.Background()); err != nil {
		cli.Fprintln(os.Stderr, err)
		cli.OsExit(1)
	}
}
