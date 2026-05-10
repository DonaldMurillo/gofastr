// kiln is the Kiln runtime CLI.
//
// Subcommands:
//
//	kiln serve         HTTP only: panel + SSE + REST tool dispatch + MCP at /mcp
//	kiln mcp           HTTP + MCP server on stdio (for subprocess harnesses)
//	kiln acp           HTTP + ACP server on stdio (for ACP-attached harnesses)
//
// In stdio modes (mcp, acp) the HTTP panel still runs in the background so
// the user can watch the world build live in their browser. Logging goes
// to stderr in stdio modes so stdout stays clean for the JSON-RPC framing.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	_ "github.com/mattn/go-sqlite3"

	"github.com/gofastr/gofastr/kiln/agent/acp"
	kilnmcp "github.com/gofastr/gofastr/kiln/agent/mcp"
	"github.com/gofastr/gofastr/kiln/chat"
	"github.com/gofastr/gofastr/kiln/db"
	"github.com/gofastr/gofastr/kiln/journal"
	"github.com/gofastr/gofastr/kiln/live"
	"github.com/gofastr/gofastr/kiln/protocol"
	"github.com/gofastr/gofastr/framework"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	switch cmd {
	case "serve":
		os.Exit(run(args, false, false))
	case "mcp":
		os.Exit(run(args, true, false))
	case "acp":
		os.Exit(run(args, false, true))
	case "agent":
		os.Exit(runAgent(args))
	case "freeze":
		os.Exit(runFreeze(args))
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `kiln — Kiln runtime

Usage:
  kiln agent [pi-args…]   Turnkey: start kiln serve, install skill, exec pi
  kiln serve [flags]      HTTP server with panel + MCP at /mcp
  kiln mcp   [flags]      HTTP + MCP over stdio
  kiln acp   [flags]      HTTP + ACP over stdio
  kiln freeze [flags]     Read journal, emit entities/*.json + world.json
                          to a target dir for "graduate to source" commits
                          --diff to print a review summary instead of writing

Flags:
  --addr value          HTTP listen address (default ":8765")
  --journal path        Path to JSONL journal (default: .kiln.session.jsonl)
  --agent value         Spawn an agent per chat_user event:
                          claude-code | pi | codex   built-in adapters (BYO auth)
                          auto                       first installed of the above
                          none                       explicit no-agent (default)
                          "<freeform cmd>"           custom command, prompt appended
  --no-http             Skip the HTTP server in stdio modes
  --keep-db             Don't delete the ephemeral SQLite on exit

Examples:
  kiln agent "build me a blog"           # turnkey pi launcher
  kiln serve --agent claude-code         # use Claude Code (~/.claude auth)
  kiln serve --agent auto                # whichever CLI you have installed
  kiln serve --addr :7777
  kiln mcp --journal ./session.jsonl
  kiln acp --no-http

Wire into Claude Code MCP settings:
  {
    "mcpServers": {
      "kiln": { "command": "kiln", "args": ["mcp", "--no-http"] }
    }
  }
`)
}

type runOptions struct {
	addr     string
	journal  string
	noHTTP   bool
	keepDB   bool
	agentCmd string
}

func parseFlags(args []string) runOptions {
	fs := flag.NewFlagSet("kiln", flag.ExitOnError)
	addr := fs.String("addr", ":8765", "HTTP listen address")
	journalPath := fs.String("journal", ".kiln.session.jsonl", "JSONL journal path (use :memory: to disable persistence)")
	noHTTP := fs.Bool("no-http", false, "Skip the HTTP server in stdio modes")
	keepDB := fs.Bool("keep-db", false, "Don't delete the ephemeral SQLite on exit")
	agentCmd := fs.String("agent", "", `Agent to spawn on each chat_user event. Accepts:
  claude-code | pi | codex   — built-in adapter (uses your existing CLI auth)
  auto                       — pick the first installed from the list above
  none                       — explicitly run no agent (default if unset)
  "<freeform cmd>"           — custom: e.g. "pi -p --model glm-5.1"
KILN_URL is injected into the env so the agent can drive the runtime.`)
	_ = fs.Parse(args)
	return runOptions{addr: *addr, journal: *journalPath, noHTTP: *noHTTP, keepDB: *keepDB, agentCmd: *agentCmd}
}

// run boots the runtime once (Live + Tools + Chat) and then drives a
// transport. mcpStdio and acpStdio are mutually exclusive. When both
// are false, the HTTP server is the only transport.
func run(args []string, mcpStdio, acpStdio bool) int {
	opts := parseFlags(args)
	stdioMode := mcpStdio || acpStdio

	// Logging goes to stderr in stdio modes so stdout stays clean for
	// JSON-RPC framing.
	logger := log.New(os.Stderr, "[kiln] ", log.LstdFlags)

	// Ephemeral SQLite scoped to the session.
	d, dbCleanup, err := db.EphemeralSQLite("kiln")
	if err != nil {
		logger.Printf("ephemeral db: %v", err)
		return 1
	}
	if !opts.keepDB {
		defer dbCleanup()
	}

	// Journal: defaults to .kiln.session.jsonl in cwd so the world
	// survives restart. `--journal :memory:` opts out for ephemeral runs.
	var j journal.Journal
	if opts.journal == ":memory:" || opts.journal == "" {
		j = journal.NewMemory()
		logger.Printf("journal:   (in-memory; world is lost on restart)")
	} else {
		jj, err := journal.OpenJSONL(opts.journal)
		if err != nil {
			logger.Printf("open journal %s: %v", opts.journal, err)
			return 1
		}
		j = jj
		defer jj.Close()
		logger.Printf("journal:   %s", opts.journal)
	}

	factory := func() *framework.App {
		return framework.NewApp(framework.WithDB(d))
	}
	l, err := live.New(j, factory)
	if err != nil {
		logger.Printf("live.New: %v", err)
		return 1
	}
	tools := protocol.New(l)

	// Mount chat panel + SSE on the auxiliary router so they survive
	// world rebuilds. The host fallback HTML (the floating widget shell)
	// is installed on Live so any unmatched URL serves it.
	//
	// Two registrations:
	//   - chat.New + Mount: legacy widget.js routes (kept transitionally)
	//   - chat.MountPanel:  new core-ui/widget-driven panel (primary)
	// host.html is updated to load the new bootstrap; the legacy routes
	// remain so existing integrations don't 404 mid-migration.
	// AdapterStore is created up-front so the panel modal's
	// agent_list_html signal can read live adapter state at hydration
	// time. mountAgentRoutes still owns the /kiln/agent HTTP surface.
	adapter, adapterOK := resolveAdapter(opts.agentCmd)
	store := NewAdapterStore(adapter)
	mountAgentRoutes(l.Aux(), store, l.Notify)

	chatSrv := chat.New(l, tools)
	chatSrv.Mount(l.Aux())
	chat.MountPanel(l.Aux(), l, tools, func() any { return agentState(store) })
	l.SetFallbackFunc(chat.HostHTMLForLive(l))

	// MCP over HTTP — Kiln's tool surface (add_entity, undo, etc.) at
	// /mcp; framework per-entity tools (auto-registered when an entity
	// has mcp:true) at /mcp/app, served from the current rebuilt app.
	mcpSrv, err := kilnmcp.NewServer(tools)
	if err != nil {
		logger.Printf("mcp.NewServer: %v", err)
		return 1
	}
	l.Aux().Handle("POST", "/mcp", mcpSrv)
	l.Aux().Handle("GET", "/mcp", mcpSrv)
	appMCP := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		app := l.App()
		if app == nil || app.MCP == nil {
			http.Error(w, "no app mcp server", http.StatusServiceUnavailable)
			return
		}
		app.MCP.ServeHTTP(w, r)
	})
	l.Aux().Handle("POST", "/mcp/app", appMCP)
	l.Aux().Handle("GET", "/mcp/app", appMCP)

	ctx, cancel := signalContext()
	defer cancel()

	// Start HTTP server unless --no-http was explicitly requested.
	if !(stdioMode && opts.noHTTP) {
		go runHTTP(ctx, logger, opts.addr, l)
		// Don't print startup banner on stdout in stdio modes.
		printBanner(logger, opts.addr, stdioMode)
	}

	// Optional in-process agent watcher: spawn the resolved adapter
	// once per chat_user event with KILN_URL injected. The store/routes
	// were registered earlier (above) so the panel modal can read state.
	if adapterOK {
		// Sync the skill so adapters that read it (claude-code, pi via
		// ~/.claude/skills/kiln/) get the current version of the
		// framework knowledge.
		if path, err := installSkill(); err == nil {
			logger.Printf("skill:     %s (synced)", path)
		} else {
			logger.Printf("skill install: %v (continuing)", err)
		}
		go runAgentWatcher(ctx, logger, l, tools, store, opts.addr)
		logger.Printf("agent:     %s", adapter.Display)
	} else if !stdioMode {
		// Even without a startup adapter, run the watcher so a runtime
		// switch via /kiln/agent immediately starts dispatching turns.
		go runAgentWatcher(ctx, logger, l, tools, store, opts.addr)
		switch opts.agentCmd {
		case "":
			logger.Printf("agent:     (none — pass --agent auto to pick an installed CLI,")
			logger.Printf("            or --agent claude-code|pi|codex to be explicit)")
		case "auto":
			logger.Printf("agent:     auto-detect found nothing on PATH (claude-code, pi, codex)")
			logger.Printf("           — install one or pass --agent \"<full cmd>\"")
		case "none":
			logger.Printf("agent:     (none — explicit)")
		default:
			logger.Printf("agent:     %q is not a known adapter and its binary isn't on PATH", opts.agentCmd)
			logger.Printf("           — pass --agent claude-code|pi|codex|auto|none")
		}
	}

	switch {
	case mcpStdio:
		// MCP over stdio. core/mcp.Server already implements this.
		if err := mcpSrv.ServeStdio(ctx, os.Stdin, os.Stdout); err != nil {
			logger.Printf("mcp stdio: %v", err)
			return 1
		}
	case acpStdio:
		acpSrv := acp.New(tools)
		if err := acpSrv.Serve(ctx, os.Stdin, os.Stdout); err != nil {
			logger.Printf("acp stdio: %v", err)
			return 1
		}
	default:
		<-ctx.Done()
	}
	return 0
}

func runHTTP(ctx context.Context, logger *log.Logger, addr string, l *live.Live) {
	srv := &http.Server{
		Addr:    addr,
		Handler: l,
	}
	go func() {
		<-ctx.Done()
		logger.Printf("http: shutting down")
		_ = srv.Shutdown(context.Background())
	}()
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Printf("http: %v", err)
	}
}

func printBanner(logger *log.Logger, addr string, stdioMode bool) {
	host := addr
	if len(host) > 0 && host[0] == ':' {
		host = "localhost" + host
	}
	logger.Printf("Kiln runtime ready — widget floats on every URL")
	logger.Printf("  open:     http://%s/", host)
	logger.Printf("  events:   http://%s/.kiln/events", host)
	logger.Printf("  tool API: POST http://%s/kiln/tool/{name}", host)
	logger.Printf("  MCP HTTP: http://%s/mcp", host)
	if stdioMode {
		logger.Printf("  stdio transport active — JSON-RPC frames on stdin/stdout")
	}
}

// signalContext returns a context cancelled on SIGINT/SIGTERM.
func signalContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-ch
		cancel()
	}()
	return ctx, cancel
}
