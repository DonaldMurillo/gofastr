// Command kiln runs the Kiln runtime (serve, mcp, acp), the turnkey agent
// launcher and freeze. See framework/docs/content/kiln.md.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"

	"github.com/DonaldMurillo/gofastr/core/netguard"
	"github.com/DonaldMurillo/gofastr/framework"
	kilnacp "github.com/DonaldMurillo/gofastr/kiln/acp"
	kilnmcp "github.com/DonaldMurillo/gofastr/kiln/agent/mcp"
	"github.com/DonaldMurillo/gofastr/kiln/chat"
	"github.com/DonaldMurillo/gofastr/kiln/db"
	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/live"
	"github.com/DonaldMurillo/gofastr/kiln/protocol"
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
	fmt.Fprint(os.Stderr, `kiln: Kiln runtime

Usage:
  kiln agent [omp-args…]  Turnkey: start kiln serve, install skill, exec OMP/GLM-5.2
  kiln serve [flags]      HTTP server with panel + MCP at /mcp
  kiln mcp   [flags]      HTTP + MCP over stdio
  kiln acp   [flags]      HTTP + ACP over stdio
  kiln freeze [flags]     Read journal, emit gofastr.yml + world.json
                          for one-shot owned-Go generation
                          --diff to print a review summary instead of writing

Flags:
  --addr value          HTTP listen address (default "127.0.0.1:8765",
                          loopback-only; the tool API is unauthenticated:
                          pass 0.0.0.0:8765 to expose it deliberately)
  --journal path        Path to JSONL journal (default: .kiln.session.jsonl)
  --agent value         Spawn an agent per chat_user event:
                          omp | claude-code | pi | codex  built-in adapters (BYO auth)
                          auto                       first installed of the above
                          none                       explicit no-agent (default)
                          "<freeform cmd>"           custom command, prompt appended
  --no-http             Skip the HTTP server in stdio modes
  --keep-db             Don't delete the ephemeral SQLite on exit

Examples:
  kiln agent -p "build me a blog"        # turnkey OMP/GLM-5.2 launcher
  kiln serve --agent omp                 # panel-driven OMP/GLM-5.2 turns
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
	addr        string
	journal     string
	noHTTP      bool
	keepDB      bool
	agentCmd    string
	allowCustom bool
}

func parseFlags(args []string) runOptions {
	fs := flag.NewFlagSet("kiln", flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:8765", "HTTP listen address (loopback by default; the tool API is unauthenticated: pass 0.0.0.0:8765 to expose it deliberately)")
	journalPath := fs.String("journal", ".kiln.session.jsonl", "JSONL journal path (use :memory: to disable persistence)")
	noHTTP := fs.Bool("no-http", false, "Skip the HTTP server in stdio modes")
	keepDB := fs.Bool("keep-db", false, "Don't delete the ephemeral SQLite on exit")
	allowCustom := fs.Bool("allow-custom-agent", false, "Permit POST /kiln/agent name=\"custom\" to choose the spawned command. Off by default: it is request-borne argv selection, i.e. remote code execution for anything that can reach the tool API.")
	agentCmd := fs.String("agent", "", `Agent to spawn on each chat_user event. Accepts:
  omp | claude-code | pi | codex  built-in adapters (BYO auth)
  auto                       : pick the first installed from the list above
  none                       : explicitly run no agent (default if unset)
  "<freeform cmd>"           : custom, e.g. "omp -p --model glm-5.2"
KILN_URL is injected into the env so the agent can drive the runtime.`)
	_ = fs.Parse(args)
	return runOptions{addr: *addr, journal: *journalPath, noHTTP: *noHTTP, keepDB: *keepDB, agentCmd: *agentCmd, allowCustom: *allowCustom}
}

func run(args []string, mcpStdio, acpStdio bool) int {
	opts := parseFlags(args)
	stdioMode := mcpStdio || acpStdio

	kilnLoopbackBound = isLoopbackBindAddr(opts.addr)
	allowCustomAgent = opts.allowCustom

	logger := log.New(os.Stderr, "[kiln] ", log.LstdFlags)

	d, dbCleanup, err := db.EphemeralSQLite("kiln")
	if err != nil {
		logger.Printf("ephemeral db: %v", err)
		return 1
	}
	if !opts.keepDB {
		defer dbCleanup()
	}

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

	adapter, adapterOK := resolveAdapter(opts.agentCmd)
	store := NewAdapterStore(adapter)
	mountAgentRoutes(l.Aux(), store, l.Notify)

	chatSrv := chat.New(l, tools)
	chatSrv.Mount(l.Aux())
	chat.MountPanel(l.Aux(), l, tools, func() any { return agentState(store) })
	l.SetFallbackFunc(chat.HostHTMLForLive(l))

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

	var watcherDone chan struct{}
	defer func() {
		if watcherDone != nil {
			<-watcherDone
		}
	}()
	ctx, cancel := signalContext()
	defer cancel()
	startWatcher := func() {
		watcherDone = make(chan struct{})
		go func() {
			defer close(watcherDone)
			runAgentWatcher(ctx, logger, l, tools, store, opts.addr)
		}()
	}

	if !(stdioMode && opts.noHTTP) {
		go runHTTP(ctx, logger, opts.addr, l)
		printBanner(logger, opts.addr, stdioMode)
	}

	if adapterOK {
		if path, err := installSkill(); err == nil {
			logger.Printf("skill:     %s (synced)", path)
		} else {
			logger.Printf("skill install: %v (continuing)", err)
		}
		startWatcher()
		logger.Printf("agent:     %s", adapter.Display)
	} else if !stdioMode {
		startWatcher()
		switch opts.agentCmd {
		case "":
			logger.Printf("agent:     (none: pass --agent auto to pick an installed CLI,")
			logger.Printf("            or --agent omp|claude-code|pi|codex to be explicit)")
		case "auto":
			logger.Printf("agent:     auto-detect found nothing on PATH (omp, claude-code, pi, codex)")
			logger.Printf("           : install one or pass --agent \"<full cmd>\"")
		case "none":
			logger.Printf("agent:     (none: explicit)")
		default:
			logger.Printf("agent:     %q is not a known adapter and its binary isn't on PATH", opts.agentCmd)
			logger.Printf("           : pass --agent omp|claude-code|pi|codex|auto|none")
		}
	}

	switch {
	case mcpStdio:
		if err := mcpSrv.ServeStdio(ctx, os.Stdin, os.Stdout); err != nil && ctx.Err() == nil {
			logger.Printf("mcp stdio: %v", err)
			return 1
		}
	case acpStdio:
		acpSrv := kilnacp.NewServer(tools)
		if err := acpSrv.Serve(ctx, os.Stdin, os.Stdout); err != nil && ctx.Err() == nil {
			logger.Printf("acp stdio: %v", err)
			return 1
		}
	default:
		<-ctx.Done()
	}
	return 0
}

func originGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if kilnLoopbackBound && !netguard.IsLoopbackAuthority(r.Host) {
			http.Error(w, "forbidden: unexpected Host (DNS-rebinding guard)", http.StatusForbidden)
			return
		}
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && !sameOrigin(origin, r.Host) {
			http.Error(w, "cross-origin request refused", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isLoopbackBindAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	if host == "" {
		return false
	}
	return netguard.IsLoopbackAuthority(host)
}

var kilnLoopbackBound bool

var allowCustomAgent bool

func sameOrigin(origin, host string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	return u.Host == host
}

func runHTTP(ctx context.Context, logger *log.Logger, addr string, l *live.Live) {
	srv := &http.Server{
		Addr:    addr,
		Handler: originGuard(l),
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
	logger.Printf("Kiln runtime ready: widget floats on every URL")
	logger.Printf("  open:     http://%s/", host)
	logger.Printf("  events:   http://%s/.kiln/events", host)
	logger.Printf("  tool API: POST http://%s/kiln/tool/{name}", host)
	logger.Printf("  MCP HTTP: http://%s/mcp", host)
	if stdioMode {
		logger.Printf("  stdio transport active: JSON-RPC frames on stdin/stdout")
	}
}

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
