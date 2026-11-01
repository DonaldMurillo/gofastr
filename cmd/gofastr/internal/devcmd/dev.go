package devcmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/gates"
	"github.com/DonaldMurillo/gofastr/framework/isolation"
)

// BuildDevChildEnv produces the env slice handed to the rebuilt server
// process. It drops every pre-existing GOFASTR_DEV entry and prepends
// GOFASTR_DEV=1, necessary because:
//
//   - macOS getenv returns the LAST occurrence; appending wins.
//   - Linux glibc getenv returns the FIRST occurrence; a parent
//     GOFASTR_DEV=0 would silently defeat the override.
//
// Dropping duplicates and prepending makes the override platform-
// independent and immune to a user's prior export.
//
// It also injects PORT=<addr> so the generated scaffold's getEnv("PORT", …)
// binds the address `gofastr dev --addr` resolved; the scaffold never
// parses argv, so without this the child silently binds the committed .env
// default while the parent banner reports the requested address.
func BuildDevChildEnv(parent []string, addr string) []string {
	out := make([]string, 0, len(parent)+2)
	out = append(out, "GOFASTR_DEV=1")
	for _, kv := range parent {
		if strings.HasPrefix(kv, "GOFASTR_DEV=") {
			continue
		}
		// A stale PORT in the parent shell (or an inherited .env export)
		// would shadow the --addr this run resolved; drop it so the
		// injected PORT below is the single value the child sees.
		if addr != "" && strings.HasPrefix(kv, "PORT=") {
			continue
		}
		out = append(out, kv)
	}
	// Inject the resolved listen address as PORT. The generated scaffold
	// binds getEnv("PORT", "localhost:8080") and never parses argv, so the
	// --addr flag alone reaches nothing; this is what makes the child
	// actually listen on the requested address. The framework's dotenv
	// auto-load never clobbers an existing process var, so this also wins
	// over the committed .env default. Dedup + a single PORT entry keeps
	// the override platform-independent (Linux getenv first-wins, macOS
	// last-wins).
	if addr != "" {
		out = append(out, "PORT="+addr)
	}
	return out
}

func RunDev(args []string) {
	if cli.HasHelpFlag(args) {
		printDevUsage()
		return
	}
	// pkg is the package to build, relative to dir. It defaults to dir itself,
	// which is right for the scaffold layout (main at the project root). Apps
	// that keep main under cmd/<name>/ need the two to differ: the build target
	// is the command, but the watch root and the server's cwd must stay at the
	// project root; otherwise the watcher misses internal/ and relative paths
	// (sqlite db_url, static dirs) resolve against the command dir instead.
	addr, dir, pkg, noA11y, err := parseDevFlags(args)
	if err != nil {
		cli.Fail("%v", err)
		printDevUsage()
		cli.OsExit(1)
		return
	}

	runtimeIsolation, resolvedAddr, err := resolveDevIsolation(dir, addr)
	if err != nil {
		cli.Fail("Isolation failed: %v", err)
		cli.OsExit(1)
	}

	fmt.Printf("\n  %s Dev server with hot reload\n\n", cli.Bold("GoFastr"))
	cli.Info("Watching %s for changes (.go, .js, .css, .html, .md)...", dir)
	if pkg != "." {
		cli.Info("Building %s", pkg)
	}
	if runtimeIsolation.Active() && resolvedAddr != addr {
		cli.Info("Isolation %s remapped http://%s -> http://%s", runtimeIsolation.ID(), addr, resolvedAddr)
	} else {
		cli.Info("Server at http://%s", resolvedAddr)
	}
	fmt.Println()

	// Hold the address until the first build is up, and whenever the app
	// is down after it. A busy address fails here, before any build.
	hold, err := holdDevAddr(resolvedAddr)
	if err != nil {
		cli.Fail("%v", err)
		cli.OsExit(1)
	}

	var (
		mu      sync.Mutex
		server  *exec.Cmd
		stop    = make(chan struct{})
		reload  = make(chan struct{}, 1)
		signals = make(chan os.Signal, 1)
		// shutdown closes on the first SIGINT/SIGTERM. A channel that
		// closes, not the signal channel itself, so a build in progress
		// and the main loop both see the one signal: a build no longer
		// runs to the end, and starts the app, before the loop reads it.
		shutdown = make(chan struct{})
	)

	// watchCtx ends with the signal too: the watcher's go list can
	// stall on a module proxy, and shutdown does not wait for it.
	watchCtx, endWatch := context.WithCancel(context.Background())
	defer endWatch()
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-signals
		close(shutdown)
		endWatch()
	}()

	exit := func() {
		fmt.Println()
		cli.Info("Shutting down...")
		KillServer(&mu, &server)
		hold.release()
		// Remove the whole minted dir: the binary inside plus the dir
		// itself, so restarts don't accumulate either.
		if dir := DevServerBinDir(); dir != "" {
			_ = os.RemoveAll(dir)
		}
	}

	// Scanned before the first build: FirstSnapshot keeps these times,
	// so a save made during the build or the graph read rebuilds.
	since := time.Now()
	before := ScanModTimes(dir)

	// Build and start the server initially
	if !buildAndServe(dir, pkg, resolvedAddr, runtimeIsolation, &mu, &server, noA11y, hold, shutdown) && !closed(shutdown) {
		cli.Fail("Initial build failed. Fixing and saving will retry")
	}
	if closed(shutdown) {
		// Stopped during the first build: no watcher to start.
		exit()
		return
	}

	// File watcher goroutine: polls for .go file changes. The watch
	// keeps every directory the build imports from, gen/ included; its
	// graph is read after the first build, which fetched the modules.
	watch := NewWatch(watchCtx, dir, pkg, nil)
	go func() {
		poll := watch.Poller(before, since)
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				if poll.Changed() {
					select {
					case reload <- struct{}{}:
					default: // already queued
					}
				}
			}
		}
	}()

	// Contract findings arrive with each reload, scoped to what changed.
	// Constructed once so it can suppress an unchanged report rather than
	// reprinting the same lines on every save.
	contractWatch := NewDevContractWatch(dir)

	// Main loop: wait for reload or shutdown
	for {
		select {
		case <-shutdown:
			exit()
			close(stop)
			return
		case <-reload:
			fmt.Println()
			cli.Info("Change detected; rebuilding...")
			KillServer(&mu, &server)
			hold.show(holdBuilding, "")
			if buildAndServe(dir, pkg, resolvedAddr, runtimeIsolation, &mu, &server, noA11y, hold, shutdown) {
				cli.Success("Reloaded!")
				// The save may have added an import: re-read the graph,
				// behind the restart.
				go watch.Refresh()
				// After the reload, never before it: analysis takes about
				// a second, and a second added to every save is the
				// difference between a loop people use and one they turn
				// off. Runs behind the restart and prints when ready.
				contractWatch.Run()
			} else if !closed(shutdown) {
				cli.Fail("Build failed. Fixing and saving will retry")
			}
		}
	}
}

// closed reports whether ch is closed, without blocking.
func closed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// parseDevFlags parses dev's flags in both the --addr=x and the --addr x
// form (cli.ParseFlags). -p <port> is shorthand for --addr localhost:<port>;
// giving both is refused rather than letting one silently win.
func parseDevFlags(args []string) (addr, dir, pkg string, noA11y bool, err error) {
	addr, dir, pkg = "localhost:8080", ".", "."
	var port string
	seen, err := cli.ParseFlags(args,
		map[string]*string{"--addr": &addr, "-p": &port, "--dir": &dir, "--pkg": &pkg},
		map[string]*bool{"--no-a11y": &noA11y})
	if err != nil {
		return addr, dir, pkg, noA11y, err
	}
	if seen["-p"] {
		if seen["--addr"] {
			return addr, dir, pkg, noA11y, fmt.Errorf("pass --addr or -p, not both")
		}
		addr = "localhost:" + port
	}
	return addr, dir, pkg, noA11y, nil
}

// SplitFlagEqual splits "--name=value" into ("--name", "value", true). A bare
// "--name" (no '=') returns ("--name", "", false) so the caller can take the
// value from the next argument.
func SplitFlagEqual(arg string) (name, value string, hasValue bool) {
	if idx := strings.IndexByte(arg, '='); idx >= 0 {
		return arg[:idx], arg[idx+1:], true
	}
	return arg, "", false
}

func resolveDevIsolation(dir, addr string) (*isolation.Runtime, string, error) {
	runtimeIsolation, err := isolation.Resolve(dir)
	if err != nil {
		return nil, "", err
	}
	resolvedAddr, err := runtimeIsolation.Addr(addr)
	if err != nil {
		return nil, "", err
	}
	return runtimeIsolation, resolvedAddr, nil
}

// DevServerBinDir is the per-process 0700 directory under the temp root
// that holds the rebuilt server binary. Minted once per process with
// os.MkdirTemp so every caller (each rebuild's build -o and exec, the
// shutdown cleanup) agrees on one unguessable path: a predictable name
// in the shared temp root let a local co-user pre-plant a symlink and
// swap the dev server binary between build and exec (CWE-377).
var DevServerBinDir = sync.OnceValue(func() string {
	dir, err := os.MkdirTemp("", "gofastr-dev-server-")
	if err != nil {
		return ""
	}
	return dir
})

// DevServerBinaryPath is the per-process temp path the rebuilt server is
// compiled to, inside the private 0700 dir above. The isolation ID keeps
// concurrent isolated dev instances distinct inside the same dir.
func DevServerBinaryPath(runtimeIsolation *isolation.Runtime) string {
	dir := DevServerBinDir()
	if dir == "" {
		return "" // MkdirTemp failed; buildAndServe refuses on empty
	}
	name := "server"
	if runtimeIsolation.Active() {
		name += "-" + runtimeIsolation.ID()
	}
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(dir, name)
}

// holdDevAddr starts the hold on the dev address. An address with port
// 0 has no fixed place for a browser to wait on, so it gets no hold.
func holdDevAddr(addr string) (*devHold, error) {
	if _, port, err := net.SplitHostPort(addr); err != nil || port == "0" {
		return nil, nil
	}
	hold, err := newDevHold(addr)
	if addrInUse(err) {
		return nil, fmt.Errorf("%s is already in use. If another gofastr dev serves it, use that one: it rebuilds on every save. Otherwise stop what holds the port or pass --addr", addr)
	}
	if err != nil {
		cli.Warn("Could not hold %s during builds: %v", addr, err)
		return nil, nil
	}
	return hold, nil
}

// addrInUse reports a bind refused because something already listens
// there. Windows reports WSAEADDRINUSE (10048), which syscall.EADDRINUSE
// does not match on that platform.
func addrInUse(err error) bool {
	var errno syscall.Errno
	return errors.Is(err, syscall.EADDRINUSE) || (errors.As(err, &errno) && errno == 10048)
}

// devA11yGate runs the same static accessibility lint `gofastr build`
// enforces, against the dev watch root. Kept as its own decision func so
// the skip semantics are testable without compiling a project. It also
// copies the report to out, for the hold page.
func devA11yGate(dir string, noA11y bool, out io.Writer) bool {
	if noA11y {
		return true
	}
	if !gates.BuildA11yGateTo(dir, io.MultiWriter(os.Stdout, out), io.MultiWriter(os.Stderr, out)) {
		const msg = "Accessibility lint failed. Fix the findings above (guided), or run with --no-a11y"
		cli.Fail(msg)
		fmt.Fprintln(out, msg)
		return false
	}
	return true
}

// buildAndServe runs the gates, builds, and starts the app. A failure
// leaves hold showing the output; a nil hold (tests on :0) skips that.
//
// interrupt ends a build in progress: when it closes, the build is
// killed and buildAndServe returns false without starting the app. Nil
// never interrupts.
func buildAndServe(dir, pkg, addr string, runtimeIsolation *isolation.Runtime, mu *sync.Mutex, cmd **exec.Cmd, noA11y bool, hold *devHold, interrupt <-chan struct{}) bool {
	out := &holdOutput{}
	if !buildGatesAndBinary(dir, pkg, runtimeIsolation, mu, cmd, noA11y, out, interrupt) {
		hold.show(holdBuildFailed, out.String())
		return false
	}
	tmpBin := DevServerBinaryPath(runtimeIsolation)

	// Start the server. GOFASTR_DEV=1 signals to framework.NewApp +
	// uihost.New that this process is under `gofastr dev`, so they
	// auto-wire the livereload SSE endpoint and client script. The
	// host doesn't need any code change to get browser reload, and
	// production deployments don't accidentally serve it because
	// GOFASTR_ENV=production is checked as a kill switch.
	childEnv := BuildDevChildEnv(runtimeIsolation.Env(os.Environ()), addr)
	runCmd := exec.Command(tmpBin, "--addr", addr)
	// Run the server in the project dir, the same cwd it gets when run by
	// hand, so relative paths (sqlite db_url, static dir) resolve against
	// the project, and the app's own worktree-isolation lookup sees the
	// project's location rather than wherever `gofastr dev` was launched.
	runCmd.Dir = dir
	runCmd.Stdout = os.Stdout
	runCmd.Stderr = os.Stderr
	runCmd.Env = childEnv

	mu.Lock()
	*cmd = runCmd
	mu.Unlock()

	// Hand the address to the app: it binds it itself.
	hold.release()
	if err := runCmd.Start(); err != nil {
		cli.Fail("Failed to start: %v", err)
		hold.show(holdBuildFailed, fmt.Sprintf("Failed to start the app: %v", err))
		return false
	}

	// Wait for it in background so we can detect crashes. The writer is
	// captured at spawn: this goroutine can outlive the caller, and the
	// coverage tests swap os.Stdout around buildAndServe; reading the
	// global here later would race that restore.
	go func(stdout *os.File) {
		err := runCmd.Wait()
		if err != nil {
			fmt.Fprintf(stdout, "\n%s\n", cli.InfoString("Server exited"))
		}
		// killServer clears *cmd under mu before this can read it, so an
		// app still current here stopped on its own: hold the address.
		// The show runs under mu too. Unlocked, a kill and rebuild could
		// land between the check and the show, and this stale exit would
		// take the address back from the app that rebuild started.
		mu.Lock()
		defer mu.Unlock()
		if *cmd == runCmd {
			status := "the app exited"
			if err != nil {
				status = err.Error()
			}
			hold.show(holdExited, status)
		}
	}(os.Stdout)

	return true
}

// buildGatesAndBinary runs the rebuild's gates and go build, copying
// their output to out as well as the terminal.
func buildGatesAndBinary(dir, pkg string, runtimeIsolation *isolation.Runtime, mu *sync.Mutex, cmd **exec.Cmd, noA11y bool, out io.Writer, interrupt <-chan struct{}) bool {
	// Same posture as `gofastr build`: the static a11y lint gates the
	// rebuild by default. Failing here is a build failure: the watch
	// loop keeps running and the next save retries.
	if !devA11yGate(dir, noA11y, out) {
		return false
	}
	// Same posture as `gofastr build`: the .ui.go hydration-sandbox lint
	// (no goroutines/channels/unsafe imports) gates the rebuild too, and is
	// not skipped by --no-a11y; a violating .ui.go breaks hydration.
	if !gates.BuildSandboxGateTo(dir, io.MultiWriter(os.Stdout, out), io.MultiWriter(os.Stderr, out)) {
		return false
	}

	tmpBin := DevServerBinaryPath(runtimeIsolation)
	if tmpBin == "" {
		const msg = "Could not create a private temp dir for the dev server binary"
		cli.Fail(msg)
		fmt.Fprintln(out, msg)
		return false
	}
	buildCmd := exec.Command("go", "build", "-o", tmpBin, pkg)
	buildCmd.Dir = dir // Run from the project dir so go build resolves the local module.
	buildCmd.Stdout = io.MultiWriter(os.Stdout, out)
	buildCmd.Stderr = io.MultiWriter(os.Stderr, out)
	// go build's compile children exit with it; the delay only bounds a
	// stray one holding the output pipe open.
	buildCmd.WaitDelay = 5 * time.Second

	mu.Lock()
	*cmd = buildCmd
	mu.Unlock()

	return RunBuildWithHeartbeat(buildCmd, dir, interrupt) == nil
}

// RunBuildWithHeartbeat waits out the dev build, printing a progress
// line every 10s it runs. `go build` is silent however long it takes,
// the banner above has already named an address, and under load a
// build can run for minutes after that: the heartbeat is what tells
// a reader (a person at the terminal, or the dev-loop e2e suite,
// which fails on output silence rather than a wall clock) "still
// building" from "wedged". The writer is captured at entry for the
// same reason the crash watcher below captures its copy: coverage
// tests swap os.Stdout around buildAndServe.
//
// When interrupt closes (the caller's SIGINT or SIGTERM), the build is
// killed with its children (the compiler, the linker, cgo's C
// toolchain) and ErrBuildInterrupted returned, so shutdown does not wait
// out a build whose result nobody will run. A non-nil interrupt puts the
// build in a process group of its own so the kill reaches all of them;
// the terminal's Ctrl-C then reaches only the caller, which must own
// the signal and close interrupt. A caller that does not own it passes
// nil, and the build stays in the terminal's group.
func RunBuildWithHeartbeat(buildCmd *exec.Cmd, what string, interrupt <-chan struct{}) error {
	w := os.Stdout
	start := time.Now()
	if interrupt != nil {
		ownBuildGroup(buildCmd)
	}
	// A signal before the build: nothing to start.
	if closed(interrupt) {
		return ErrBuildInterrupted
	}
	// Started here, not inside the waiting goroutine, so the interrupt
	// arm below reads a Process that Start has already set.
	if err := buildCmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- buildCmd.Wait() }()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			return err
		case <-interrupt:
			killBuild(buildCmd)
			<-done // WaitDelay bounds a child holding the output pipe
			return ErrBuildInterrupted
		case <-ticker.C:
			fmt.Fprintf(w, "%s\n", cli.InfoString("Building %s (%ds)...", what, int(time.Since(start).Seconds())))
		}
	}
}

// killGrace is how long KillServer lets the app drain after SIGTERM
// before it sends SIGKILL: shutdown hooks, the process-module drain (a
// 2s budget) and outbox flushes run in it. The framework ends open
// event streams (livereload, the SSE bus) when its drain starts, so an
// app usually exits well inside it, and KillServer returns as soon as
// it does.
var killGrace = 5 * time.Second

// exitPoll is how often KillServer looks for the process's exit.
const exitPoll = 10 * time.Millisecond

// ErrBuildInterrupted is RunBuildWithHeartbeat's answer for a build
// killed by shutdown.
var ErrBuildInterrupted = errors.New("build interrupted by shutdown")

// KillServer stops the current server process: SIGTERM, then up to
// killGrace for the app to exit, then SIGKILL. The caller must have a
// goroutine waiting on the process (both dev loops do, and a build's
// Run waits on its own): exit is observed when that wait reaps it, so a
// process nobody reaps reads as running until the grace ends.
func KillServer(mu *sync.Mutex, cmd **exec.Cmd) {
	mu.Lock()
	defer mu.Unlock()

	if *cmd != nil && (*cmd).Process != nil {
		stopProcess((*cmd).Process, killGrace)
		*cmd = nil
	}
}

// noSIGTERM is true where the OS has no SIGTERM to send (Windows); a
// test sets it to run that branch on this OS.
var noSIGTERM = runtime.GOOS == "windows"

// stopProcess sends SIGTERM and waits up to grace for p to exit before
// it kills it. Windows has no SIGTERM to send, so p is killed at once,
// and stopProcess still waits up to grace for the exit: TerminateProcess
// returns before the process is gone, and shutdown removes the binary
// dir next, which fails on an .exe still running.
func stopProcess(p *os.Process, grace time.Duration) {
	if noSIGTERM || p.Signal(syscall.SIGTERM) != nil {
		_ = p.Kill()
		waitExit(p, grace)
		return
	}
	if !waitExit(p, grace) {
		_ = p.Kill()
	}
}

// waitExit reports whether p exited within grace. Signal 0 delivers
// nothing; once the waiter has reaped p it answers os.ErrProcessDone,
// on Windows as on Unix.
func waitExit(p *os.Process, grace time.Duration) bool {
	deadline := time.Now().Add(grace)
	for time.Now().Before(deadline) {
		if errors.Is(p.Signal(syscall.Signal(0)), os.ErrProcessDone) {
			return true
		}
		time.Sleep(exitPoll)
	}
	return false
}

// Watch is the dev watcher's view of a project: the directory it walks
// and the directories it must walk whatever their name, the build
// target's own package and every package it imports from the tree.
type Watch struct {
	dir string
	pkg string
	// env is the build's environment, nil for this process's: the
	// graph go list reports depends on it (CGO_ENABLED, GOOS and build
	// tags pick files, and files pick imports).
	env []string
	// ctx ends a dependency query in progress: shutdown must not wait
	// on a go list stalled on a module proxy.
	ctx context.Context
	// refresh serializes Refresh, so a slower, older query can never
	// land after a newer one and put a stale graph back.
	refresh sync.Mutex
	// keep holds slash paths relative to dir: each package directory in
	// the target's graph and every directory above it. Nil keeps none.
	keep atomic.Pointer[map[string]bool]
	// graph counts the keep sets stored that differ from the one before,
	// so a Poller can tell a graph change from a save.
	graph atomic.Uint64
}

// NewWatch reads pkg's package graph (go list, run in dir with env, the
// build's environment; nil inherits this process's) and returns the
// watch over dir. Cancelling ctx ends a query in progress, this one and
// every later Refresh: the watch then keeps the graph it has.
func NewWatch(ctx context.Context, dir, pkg string, env []string) *Watch {
	w := &Watch{dir: dir, pkg: pkg, env: env, ctx: ctx}
	w.Refresh()
	return w
}

// listTimeout bounds the go list behind a Refresh.
const listTimeout = 30 * time.Second

// listDeps lists the directories of pkg's non-standard dependencies,
// pkg included. A variable so a test can hold one query back.
var listDeps = func(ctx context.Context, dir, pkg string, env []string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, listTimeout)
	defer cancel()
	// No -e: a package that does not load fails the query, and a failed
	// query keeps the last graph rather than a partial one.
	list := exec.CommandContext(ctx, "go", "list", "-deps", "-f", "{{if not .Standard}}{{.Dir}}{{end}}", pkg)
	list.Dir = dir
	list.Env = env
	list.WaitDelay = 5 * time.Second
	out, err := list.Output()
	if err != nil {
		return nil, err
	}
	var dirs []string
	for line := range strings.Lines(string(out)) {
		if d := strings.TrimSpace(line); d != "" {
			dirs = append(dirs, d)
		}
	}
	return dirs, nil
}

// Refresh re-reads the package graph, after a rebuild that may have
// added or dropped an import. A successful query replaces the graph, so
// an import that is gone stops keeping its directory. A failed one (a
// save that left go.mod or an import broken, or a cancelled ctx) keeps
// the graph it had: a package does not drop out of the watch because
// the tree is mid-edit. Calls run one at a time, in call order.
func (w *Watch) Refresh() {
	w.refresh.Lock()
	defer w.refresh.Unlock()
	root, err := filepath.Abs(w.dir)
	if err != nil {
		return
	}
	if real, err := filepath.EvalSymlinks(root); err == nil {
		root = real
	}
	keep := map[string]bool{}
	add := func(d string) {
		if real, err := filepath.EvalSymlinks(d); err == nil {
			d = real
		}
		rel, err := filepath.Rel(root, d)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return
		}
		for rel != "." {
			keep[filepath.ToSlash(rel)] = true
			rel = filepath.Dir(rel)
		}
	}
	// The target's own directory counts even when go list cannot run.
	if strings.HasPrefix(w.pkg, ".") {
		add(filepath.Join(root, filepath.FromSlash(w.pkg)))
	}
	dirs, err := listDeps(w.ctx, w.dir, w.pkg, w.env)
	if err != nil {
		if w.keep.Load() == nil {
			w.keep.Store(&keep)
		}
		return
	}
	for _, d := range dirs {
		add(d)
	}
	if was := w.keep.Swap(&keep); was == nil || !maps.Equal(*was, keep) {
		w.graph.Add(1)
	}
}

// skipDir reports a directory the watcher does not walk: dependencies
// (vendor, node_modules) and every dot-directory (.git, .gofastr,
// editor state, and agent worktrees under .claude/worktrees, whose
// saves would otherwise restart this server) at any depth, and build
// output (dist, gen, tmp) at the root alone. A directory the build
// target's package graph runs through is always walked.
func (w *Watch) skipDir(path, name string) bool {
	rel, err := filepath.Rel(w.dir, path)
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	if keep := w.keep.Load(); keep != nil && (*keep)[rel] {
		return false
	}
	switch name {
	case "vendor", "node_modules":
		return true
	case "dist", "gen", "tmp":
		if !strings.Contains(rel, "/") {
			return true
		}
	}
	return strings.HasPrefix(name, ".")
}

// Scan walks the directory and records the latest mod time of source
// and embedded-asset files. Go embeds .js, .css, .html, and .md at build
// time (framework docs, llm.md sources), so changes to those files also
// require a rebuild.
func (w *Watch) Scan() map[string]time.Time {
	result := make(map[string]time.Time)
	filepath.Walk(w.dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		// The root itself is walked whatever its name (".", a dot-dir
		// the developer pointed --dir at).
		if info.IsDir() && path != w.dir && w.skipDir(path, info.Name()) {
			return filepath.SkipDir
		}
		ext := filepath.Ext(path)
		switch ext {
		case ".go", ".js", ".css", ".html", ".md":
			result[path] = info.ModTime()
		}
		return nil
	})
	return result
}

// ScanModTimes scans dir with no package graph: root-level build output
// and dependency and dot-directories are skipped, everything else
// walked. The dev loops scan through NewWatch, which also keeps every
// directory the build target imports from.
func ScanModTimes(dir string) map[string]time.Time {
	return (&Watch{dir: dir}).Scan()
}

// FirstSnapshot is a watch's first snapshot. before is the scan taken
// before the first build (ScanModTimes, no package graph yet), started
// at since, and after the watch's scan once the build ran and the graph
// loaded. A file in both keeps its time from before, and a file only in
// before stays, so a save or a removal made during the build or the
// graph read differs from the next scan and rebuilds. A file only in
// after (a gen/ file the graph keeps, a file created since) counts by
// its time: one not modified since the scan began is the baseline, and
// a newer one is left out, so the next scan sees it and rebuilds.
func FirstSnapshot(before, after map[string]time.Time, since time.Time) map[string]time.Time {
	first := maps.Clone(before)
	for p, t := range after {
		if _, ok := before[p]; !ok && !t.After(since) {
			first[p] = t
		}
	}
	return first
}

// Poller is a poll loop's view of a watch: the last snapshot, the time
// the scan behind it began, and the graph it was taken under.
type Poller struct {
	w     *Watch
	prev  map[string]time.Time
	since time.Time
	graph uint64
}

// Poller starts a poll loop from before, the scan taken at since before
// the first build (FirstSnapshot).
func (w *Watch) Poller(before map[string]time.Time, since time.Time) *Poller {
	graph := w.graph.Load()
	return &Poller{w: w, prev: FirstSnapshot(before, w.Scan(), since), since: since, graph: graph}
}

// Changed scans the watch and reports whether the tree changed since the
// last scan that did. A graph change in between (a Refresh after a save
// added or dropped an import) moves files into or out of the walk, which
// is not a save: a file it brings in that is no newer than the last
// change's scan joins the snapshot, and one it drops that still exists
// leaves it, the way FirstSnapshot treats the first graph. Without that
// the first import of a root gen/ package restarted the app twice.
func (p *Poller) Changed() bool {
	graph := p.w.graph.Load()
	at := time.Now()
	curr := p.w.Scan()
	if p.w.graph.Load() != graph {
		return false // the graph moved mid-walk: the next tick rescans
	}
	if graph != p.graph {
		p.prev = rebase(p.prev, curr, p.since)
		p.graph = graph
	}
	if !Changed(p.prev, curr) {
		return false
	}
	p.prev, p.since = curr, at
	return true
}

// rebase folds a graph change into prev: a file only in curr whose time
// is not after since is baseline, and a file only in prev that is still
// on disk left the walk. A file only in curr that is newer, or one only
// in prev that is gone, stays a change.
func rebase(prev, curr map[string]time.Time, since time.Time) map[string]time.Time {
	next := maps.Clone(prev)
	for p, t := range curr {
		if _, ok := prev[p]; !ok && !t.After(since) {
			next[p] = t
		}
	}
	for p := range prev {
		if _, ok := curr[p]; !ok {
			if _, err := os.Lstat(p); err == nil {
				delete(next, p)
			}
		}
	}
	return next
}

// Changed compares two mod-time maps and returns true if any file was added, removed, or modified.
func Changed(prev, curr map[string]time.Time) bool {
	if len(prev) != len(curr) {
		return true
	}
	for path, t := range curr {
		if pt, ok := prev[path]; !ok || pt != t {
			return true
		}
	}
	return false
}
