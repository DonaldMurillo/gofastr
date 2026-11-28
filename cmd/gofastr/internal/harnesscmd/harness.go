package harnesscmd

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/hex"
	"flag"
	"fmt"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	xharness "github.com/DonaldMurillo/gofastr/framework/experimental/harness"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/client/tui"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control/inproc"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/engine"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/logging"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/profile"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/provider"
	xterm "golang.org/x/term"
)

func RunHarness(args []string) {
	if cli.HasHelpFlag(args) {
		printHarnessUsage()
		return
	}
	if len(args) > 0 && args[0] == "mcp" {
		runHarnessMCP(args[1:])
		return
	}
	if len(args) > 0 && args[0] == "creds" {
		runHarnessCreds(args[1:])
		return
	}
	fs := flag.NewFlagSet("harness", flag.ExitOnError)
	useFramework := fs.Bool("framework", false, "use the framework preset profile")
	profilePath := fs.String("profile", "", "explicit profile TOML path")
	web := fs.Bool("web", false, "force-enable the web sidecar (default: on for interactive sessions)")
	noWeb := fs.Bool("no-web", false, "disable the web sidecar")
	listen := fs.String("listen", "", "bind control-plane TCP listener on a loopback host:port")
	noListen := fs.Bool("no-listen", false, "disable Unix socket; in-process only")
	allowProjectHooks := fs.Bool("allow-project-hooks", false, "enable hooks defined in the project repo")
	autoApprove := fs.Bool("auto-approve", false, "bypass human-ack permission requirement")
	strictPerms := fs.Bool("strict-permissions", false, "disable the quiet-mode permission preset")
	allowRepoSecrets := fs.Bool("allow-repo-secrets", false, "use provider keys from this repository's .harness-secrets/env for this run without asking")
	logToStderr := fs.Bool("log-to-stderr", false, "also write logs to stderr")
	logLevel := fs.String("log-level", "", "per-component level overrides (engine=debug,...)")
	prompt := fs.String("prompt", "", "send a single prompt and exit (handy for smoke tests)")
	_ = fs.Parse(args)

	_ = noListen
	_ = autoApprove

	xdgConfig, xdgState, err := harnessDirs()
	if err != nil {
		cli.Fprintf(os.Stderr, "gofastr harness: %v\n", err)
		cli.OsExit(1)
		return
	}

	loadRepoSecrets(xdgConfig, *allowRepoSecrets, isTTY(os.Stdin) && isTTY(os.Stderr), os.Stdin, os.Stderr)

	prof, err := loadProfile(*useFramework, *profilePath)
	if err != nil {
		cli.Fprintln(os.Stderr, err)
		cli.OsExit(1)
	}

	logger := buildLogger(xdgState, *logToStderr, *logLevel)
	logger.Info("boot", "profile", prof.Name, "model", prof.DefaultModel)

	machineKey, err := machineKeyFromEnv()
	if err != nil {
		cli.Fprintln(os.Stderr, err)
		cli.OsExit(1)
	}

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
		fmt.Fprintf(os.Stderr, "gofastr harness: %s\n", noCredstoreKeyHelp)
		cli.OsExit(1)
		return
	}
	h, err := xharness.New(cfg)
	if err != nil {
		cli.Fprintf(os.Stderr, "gofastr harness: %v\n", err)
		cli.OsExit(1)
	}
	defer h.Shutdown()

	if *strictPerms {
		h.Perms.StrictPermissions = true
	}

	prov, modelID := resolveDefaultModel(h, prof)
	if prov == nil {
		cli.Fprintf(os.Stderr, "harness: no provider matches %q\n", prof.DefaultModel)
		cli.OsExit(1)
	}

	sess, err := h.CreateSession(prov, modelID)
	if err != nil {
		cli.Fprintln(os.Stderr, err)
		cli.OsExit(1)
	}

	c := inproc.New(ids.NewClientID(), control.IdentityHuman, h.Mux.EngineFor(sess).Bus, h.Mux)
	if err := h.Mux.Attach(sess, c); err != nil {
		cli.Fprintln(os.Stderr, err)
		cli.OsExit(1)
	}
	defer c.Close()

	webURL := ""
	webToken := ""
	if wantWebSidecar(*listen, *web, *noWeb, *prompt, isTTY(os.Stdin)) {
		bind := *listen
		if bind == "" {
			bind = "127.0.0.1:0"
		}
		url, token, shutdown, err := startHTTPListener(h, sess, bind)
		if err != nil {
			cli.Fprintln(os.Stderr, "harness: http listener:", err)
			cli.OsExit(1)
		}
		defer shutdown()
		webURL = url
		webToken = token
		logger.Info("http-listener", "url", url)
	}

	if *prompt != "" {
		runSingle(c, sess, *prompt)
		return
	}
	if isTTY(os.Stdin) {
		t := tui.New(c, sess)
		t.Profile = prof.Name
		t.Model = prof.DefaultModel
		t.WebURL = webURL
		t.WebToken = webToken
		if err := t.Run(context.Background()); err != nil {
			cli.Fprintln(os.Stderr, err)
		}
		return
	}
	runREPL(c, sess)
}

// wantWebSidecar reports whether the harness binds its HTTP control plane.
func wantWebSidecar(listen string, web, noWeb bool, prompt string, tty bool) bool {
	return listen != "" || web || (!noWeb && prompt == "" && tty)
}

func isTTY(f *os.File) bool {
	if f == nil {
		return false
	}
	return xterm.IsTerminal(int(f.Fd()))
}

func loadProfile(useFramework bool, explicit string) (*profile.Profile, error) {
	if explicit != "" {
		if _, err := os.Stat(explicit); err != nil {
			return nil, fmt.Errorf("profile path %q: %w", explicit, err)
		}
		return profile.Load(explicit)
	}
	name := "default"
	if useFramework {
		name = "framework"
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

func runSingle(c *inproc.Client, sess ids.SessionID, prompt string) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sub := c.Subscribe(ctx)
	if err := c.Send(ctx, control.SendInput{
		SessionID: sess,
		Content:   engine.SimpleInput(prompt),
	}); err != nil {
		cli.Fprintln(os.Stderr, err)
		return
	}
	streamOneTurn(sub)
}

func runREPL(c *inproc.Client, sess ids.SessionID) {
	fmt.Fprintln(os.Stderr, "gofastr harness: interactive REPL. Ctrl-D to exit.")
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 64*1024), 1<<20)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sub := c.Subscribe(ctx)
	for {
		fmt.Fprint(os.Stderr, "> ")
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				cli.Fprintf(os.Stderr, "\nread input: %v\n", err)
			}
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
			cli.Fprintln(os.Stderr, err)
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
				fmt.Print(textsafe.ScrubTerminal(t.Text))
			}
		case "TurnEnded":
			fmt.Println()
			return
		case "Error":
			e, _ := control.DecodeEvent(env)
			cli.Fprintf(os.Stderr, "\n[error] %v\n", e)
			return
		}
	}
}

func mustGetwd() string {
	wd, _ := os.Getwd()
	return wd
}
