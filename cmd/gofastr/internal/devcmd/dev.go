package devcmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/gates"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/project"
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
		cli.OsExit(2)
		return
	}

	if err := checkDevDir(dir); err != nil {
		cli.Fail("%v", err)
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
	signal.Notify(signals, ShutdownSignals...)
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
	go watch.Notify(before, since, stop, reload)

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

// ShutdownSignals end a dev loop cleanly: the app is stopped, the hold
// released and the binary directory removed. SIGHUP is among them: a
// closed terminal or a supervisor's hangup otherwise killed gofastr dev
// outright and left the app running on the dev address, reparented to
// init, with its binary left in the temp directory.
var ShutdownSignals = []os.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP}

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
	if err := checkDevAddr(addr); err != nil {
		return addr, dir, pkg, noA11y, err
	}
	return addr, dir, pkg, noA11y, nil
}

// checkDevAddr refuses an address no listener can take, before the loop
// starts: a port that is not a number from 0 to 65535 (an empty one bound
// a random port the banner never named), or no port at all. A bare port
// ("9000") is accepted, as isolation reads it as ":9000".
func checkDevAddr(addr string) error {
	hostport := addr
	if !strings.Contains(addr, ":") {
		hostport = ":" + addr
	}
	_, port, err := net.SplitHostPort(hostport)
	if err == nil {
		if n, perr := strconv.Atoi(port); perr != nil || n < 0 || n > 65535 || port != strconv.Itoa(n) {
			err = fmt.Errorf("port %q is not a number from 0 to 65535", port)
		}
	}
	if err != nil {
		return fmt.Errorf("invalid listen address %q: %v (want host:port, :port or -p <port>)", addr, err)
	}
	return nil
}

// checkDevDir refuses a --dir that is not a directory. The loop used to
// start on one anyway: the first build failed, and "Fixing and saving
// will retry" waited on a tree that does not exist.
func checkDevDir(dir string) error {
	fi, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("--dir %s: %w", dir, err)
	}
	if !fi.IsDir() {
		return fmt.Errorf("--dir %s is not a directory", dir)
	}
	return nil
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
	TieToParent(runCmd)

	mu.Lock()
	*cmd = runCmd
	mu.Unlock()

	// Hand the address to the app: it binds it itself.
	hold.release()
	if err := runCmd.Start(); err != nil {
		msg := fmt.Sprintf("Failed to start the app: %v", err)
		// go build -o writes a non-main package's archive without
		// complaint, and the exec of it fails with "permission denied":
		// say what is wrong instead.
		listCtx, cancel := context.WithTimeout(context.Background(), listTimeout)
		name, err := project.PackageName(listCtx, dir, pkg, 5*time.Second)
		cancel()
		if err == nil && name != "" && name != "main" {
			msg = fmt.Sprintf("%s is package %s, not a main package. gofastr dev runs a command: pass --pkg ./cmd/<name> when main lives below the project root", pkg, name)
		}
		cli.Fail("%s", msg)
		hold.show(holdBuildFailed, msg)
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
	buildCmd := BuildCommand(tmpBin, pkg)
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
