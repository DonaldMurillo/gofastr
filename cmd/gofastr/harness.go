// Package main — `gofastr harness` subcommand.
//
// Boot sequence per docs/harness-architecture.md § Lifecycle / boot.
// v0.1 flags supported:
//
//	--framework             use the framework preset profile
//	--profile <path>        explicit profile TOML
//	--web                   launch the bundled web client
//	--listen <addr>         bind a control-plane TCP listener
//	--no-listen             disable the Unix socket too (in-process only)
//	--allow-project-hooks   enable hooks in <repo>/.gofastr/harness/
//	--auto-approve          bypass human-ack permission requirement
//	--strict-permissions    no quiet-mode preset
//	--log-to-stderr         also write logs to stderr
//	--log-level <spec>      per-component level overrides
//
// v0.1 doesn't yet launch the TUI / web — those are tasks 39 and 40.
// The subcommand instead boots the engine, registers everything, and
// runs a tiny REPL so the binary is functional end-to-end.
package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	xterm "golang.org/x/term"

	xharness "github.com/DonaldMurillo/gofastr/framework/harness"
	"github.com/DonaldMurillo/gofastr/framework/harness/client/tui"
	xcontext "github.com/DonaldMurillo/gofastr/framework/harness/context"
	"github.com/DonaldMurillo/gofastr/framework/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/harness/control/inproc"
	"github.com/DonaldMurillo/gofastr/framework/harness/engine"
	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/harness/logging"
	"github.com/DonaldMurillo/gofastr/framework/harness/profile"
	"github.com/DonaldMurillo/gofastr/framework/harness/provider"
	"github.com/DonaldMurillo/gofastr/framework/harness/secrets"
)

func runHarness(args []string) {
	// `gofastr harness mcp` launches the stdio MCP server — the path
	// other MCP clients (Claude Code, Codex, Cursor) use to spawn
	// the harness as a subprocess.
	if len(args) > 0 && args[0] == "mcp" {
		runHarnessMCP(args[1:])
		return
	}
	fs := flag.NewFlagSet("harness", flag.ExitOnError)
	useFramework := fs.Bool("framework", false, "use the framework preset profile")
	profilePath := fs.String("profile", "", "explicit profile TOML path")
	web := fs.Bool("web", false, "force-enable the web sidecar (default: on for interactive sessions)")
	noWeb := fs.Bool("no-web", false, "disable the web sidecar")
	listen := fs.String("listen", "", "bind control-plane TCP listener (host:port)")
	noListen := fs.Bool("no-listen", false, "disable Unix socket; in-process only")
	allowProjectHooks := fs.Bool("allow-project-hooks", false, "enable hooks defined in the project repo")
	autoApprove := fs.Bool("auto-approve", false, "bypass human-ack permission requirement")
	strictPerms := fs.Bool("strict-permissions", false, "disable the quiet-mode permission preset")
	logToStderr := fs.Bool("log-to-stderr", false, "also write logs to stderr")
	logLevel := fs.String("log-level", "", "per-component level overrides (engine=debug,...)")
	prompt := fs.String("prompt", "", "send a single prompt and exit (handy for smoke tests)")
	_ = fs.Parse(args)

	_ = noListen
	_ = autoApprove // wiring lands with task 24 + 6 (multiplex auto-approve)

	// Load repo-local secrets (gitignored) so .harness-secrets/env
	// provides API keys without needing every invocation to export
	// them. Shell env wins on conflict.
	_, _ = secrets.LoadRepo()

	// Resolve XDG paths.
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "gofastr harness: %v\n", err)
		osExit(1)
	}
	xdgConfig := filepath.Join(home, ".config", "gofastr", "harness")
	xdgState := filepath.Join(home, ".local", "share", "gofastr", "harness")
	if err := os.MkdirAll(xdgConfig, 0o700); err != nil {
		fmt.Fprintln(os.Stderr, err)
		osExit(1)
	}
	if err := os.MkdirAll(xdgState, 0o700); err != nil {
		fmt.Fprintln(os.Stderr, err)
		osExit(1)
	}

	// Pick profile.
	prof, err := loadProfile(*useFramework, *profilePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		osExit(1)
	}

	// Logger.
	logger := buildLogger(xdgState, *logToStderr, *logLevel)
	logger.Info("boot", "profile", prof.Name, "model", prof.DefaultModel)

	machineKey, err := machineKeyFromEnv()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		osExit(1)
	}

	// Compose the harness.
	cfg := xharness.Config{
		Profile:           prof,
		WorkingDir:        mustGetwd(),
		XDGConfig:         xdgConfig,
		XDGState:          xdgState,
		Logger:            logger,
		AllowProjectHooks: *allowProjectHooks,
		CredstorePass:     os.Getenv("GOFASTR_HARNESS_PASSPHRASE"),
		MachineKey:        machineKey,
	}
	if cfg.CredstorePass == "" && len(cfg.MachineKey) == 0 {
		// Best-effort default for first-run dev convenience.
		cfg.CredstorePass = "harness-default-passphrase-change-me"
		logger.Warn("using default credstore passphrase; set GOFASTR_HARNESS_PASSPHRASE")
	}
	h, err := xharness.New(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gofastr harness: %v\n", err)
		osExit(1)
	}
	defer h.Shutdown()

	// Apply --strict-permissions to the permission engine.
	if *strictPerms {
		h.Perms.StrictPermissions = true
	}

	// Resolve provider from the profile's default_model "provider:id".
	prov, modelID := resolveDefaultModel(h, prof)
	if prov == nil {
		fmt.Fprintf(os.Stderr, "harness: no provider matches %q\n", prof.DefaultModel)
		osExit(1)
	}

	// Spawn one engine for the session.
	sess := h.CreateSession(prov, modelID)

	// Bundled inproc client.
	c := inproc.New(ids.NewClientID(), control.IdentityHuman, h.Mux.EngineFor(sess).Bus, h.Mux)
	if err := h.Mux.Attach(sess, c); err != nil {
		fmt.Fprintln(os.Stderr, err)
		osExit(1)
	}
	defer c.Close()

	// HTTP control plane: REST at /v1/*, WS at /v1/ws, SSR landing
	// page at /. Auto-enabled for interactive TTY sessions so the
	// TUI status line always shows a URL; disabled for one-shot
	// -prompt runs (no point binding a port for a single response)
	// and when -no-web is set explicitly. -listen forces an explicit
	// bind address regardless.
	wantHTTP := *listen != "" || *web ||
		(!*noWeb && *prompt == "" && isTTY(os.Stdin))
	webURL := ""
	webToken := ""
	if wantHTTP {
		bind := *listen
		if bind == "" {
			bind = "127.0.0.1:0" // ephemeral loopback
		}
		url, token, shutdown, err := startHTTPListener(h, sess, bind)
		if err != nil {
			fmt.Fprintln(os.Stderr, "harness: http listener:", err)
			osExit(1)
		}
		defer shutdown()
		webURL = url
		webToken = token
		logger.Info("http-listener", "url", url)
	}

	if *prompt != "" {
		// Single-shot mode for smoke tests.
		runSingle(h, c, sess, *prompt)
		return
	}
	// Interactive mode: launch the pure-stdlib TUI if stdin is a
	// terminal. Pipe/redirect input falls back to the bare REPL.
	if isTTY(os.Stdin) {
		t := tui.New(c, sess)
		t.Profile = prof.Name
		t.Model = prof.DefaultModel
		t.WebURL = webURL
		t.WebToken = webToken
		if err := t.Run(context.Background()); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
		return
	}
	runREPL(h, c, sess)
}

// isTTY reports whether the given file is a terminal device. Used to
// decide between the TUI and the bare REPL.
func isTTY(f *os.File) bool {
	if f == nil {
		return false
	}
	return xterm.IsTerminal(int(f.Fd()))
}

func loadProfile(useFramework bool, explicit string) (*profile.Profile, error) {
	// An explicit --profile path must exist on disk; no embedded fallback.
	if explicit != "" {
		if _, err := os.Stat(explicit); err != nil {
			return nil, fmt.Errorf("profile path %q: %w", explicit, err)
		}
		return profile.Load(explicit)
	}
	name := "default"
	path := "framework/harness/profile/default.toml"
	if useFramework {
		name = "framework"
		path = "framework/harness/profile/framework.toml"
	}
	// Prefer the on-disk copy when running inside the source tree (so
	// edits to the .toml take effect without rebuilding); fall back to
	// the copy embedded in the binary for installed/out-of-tree runs.
	if _, err := os.Stat(path); err == nil {
		return profile.Load(path)
	}
	return profile.Embedded(name)
}

func buildLogger(xdgState string, toStderr bool, levelSpec string) *logging.Logger {
	logDir := filepath.Join(xdgState, "log")
	_ = os.MkdirAll(logDir, 0o700)
	dailyWriter, err := logging.NewDailyFileWriter(logDir)
	if err != nil {
		dailyWriter = nil
	}
	var writers []io.Writer
	if dailyWriter != nil {
		writers = append(writers, dailyWriter)
	}
	if toStderr {
		writers = append(writers, os.Stderr)
	}
	out := io.MultiWriter(writers...)
	if len(writers) == 0 {
		out = io.Discard
	}
	l := logging.New(out, logging.LevelInfo)
	if levelSpec != "" {
		_ = l.ApplyOverrides(levelSpec)
	}
	return l
}

// machineKeyFromEnv reads GOFASTR_HARNESS_MACHINE_KEY and decodes it into
// a 32-byte key. It accepts three encodings of a 32-byte key:
//   - 32 raw bytes (length 32),
//   - 64 hex characters,
//   - base64 (standard or URL, with or without padding).
//
// An empty value yields (nil, nil) — no machine key configured. Any other
// value that does not decode to exactly 32 bytes returns an error so the
// caller fails loudly instead of silently downgrading to a weaker secret.
func machineKeyFromEnv() ([]byte, error) {
	raw := os.Getenv("GOFASTR_HARNESS_MACHINE_KEY")
	if raw == "" {
		return nil, nil
	}
	if len(raw) == 32 {
		return []byte(raw), nil
	}
	if len(raw) == 64 {
		if b, err := hex.DecodeString(raw); err == nil && len(b) == 32 {
			return b, nil
		}
	}
	for _, enc := range []*base64.Encoding{
		base64.StdEncoding, base64.RawStdEncoding,
		base64.URLEncoding, base64.RawURLEncoding,
	} {
		if b, err := enc.DecodeString(raw); err == nil && len(b) == 32 {
			return b, nil
		}
	}
	return nil, fmt.Errorf("GOFASTR_HARNESS_MACHINE_KEY: not a 32-byte key (raw, hex, or base64)")
}

func resolveDefaultModel(h *xharness.Harness, prof *profile.Profile) (provider.Provider, string) {
	// default_model format: "<provider>:<model-id>".
	parts := strings.SplitN(prof.DefaultModel, ":", 2)
	if len(parts) != 2 {
		return nil, ""
	}
	want := parts[0]
	for _, p := range h.Providers {
		if p.Name() == want {
			return p, parts[1]
		}
	}
	return nil, ""
}

func runSingle(h *xharness.Harness, c *inproc.Client, sess ids.SessionID, prompt string) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sub := c.Subscribe(ctx)
	if err := c.Send(ctx, control.SendInput{
		SessionID: sess,
		Content:   engine.SimpleInput(prompt),
	}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	for env := range sub {
		if env.Kind == "TextDelta" {
			td, _ := control.DecodeEvent(env)
			if t, ok := td.(control.TextDelta); ok {
				fmt.Print(t.Text)
			}
		}
		if env.Kind == "TurnEnded" {
			fmt.Println()
			return
		}
		if env.Kind == "Error" {
			e, _ := control.DecodeEvent(env)
			fmt.Fprintf(os.Stderr, "\n[error] %v\n", e)
			return
		}
	}
	_ = h
}

func runREPL(h *xharness.Harness, c *inproc.Client, sess ids.SessionID) {
	fmt.Fprintln(os.Stderr, "gofastr harness — interactive REPL. Ctrl-D to exit.")
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 64*1024), 1<<20)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sub := c.Subscribe(ctx)
	for {
		fmt.Fprint(os.Stderr, "> ")
		if !scanner.Scan() {
			return
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if err := c.Send(ctx, control.SendInput{
			SessionID: sess,
			Content:   engine.SimpleInput(line),
		}); err != nil {
			fmt.Fprintln(os.Stderr, err)
			continue
		}
		streamOneTurn(sub)
	}
}

func streamOneTurn(sub <-chan control.EventEnvelope) {
	for env := range sub {
		switch env.Kind {
		case "TextDelta":
			td, _ := control.DecodeEvent(env)
			if t, ok := td.(control.TextDelta); ok {
				fmt.Print(t.Text)
			}
		case "TurnEnded":
			fmt.Println()
			return
		case "Error":
			e, _ := control.DecodeEvent(env)
			fmt.Fprintf(os.Stderr, "\n[error] %v\n", e)
			return
		}
	}
}

func mustGetwd() string {
	wd, _ := os.Getwd()
	return wd
}

// keep references that may otherwise be reported as unused.
var (
	_ = xcontext.Reader{}
	_ = filepath.Join
)
