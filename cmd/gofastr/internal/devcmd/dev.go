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

// BuildDevChildEnv returns parent with GOFASTR_DEV=1 first and every other GOFASTR_DEV
// dropped, and, when addr is set, PORT=addr in place of any PORT parent had.
func BuildDevChildEnv(parent []string, addr string) []string {
	out := make([]string, 0, len(parent)+2)
	out = append(out, "GOFASTR_DEV=1")
	for _, kv := range parent {
		if strings.HasPrefix(kv, "GOFASTR_DEV=") {
			continue
		}
		if addr != "" && strings.HasPrefix(kv, "PORT=") {
			continue
		}
		out = append(out, kv)
	}
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

	hold, err := holdDevAddr(resolvedAddr)
	if err != nil {
		cli.Fail("%v", err)
		cli.OsExit(1)
	}

	var (
		mu       sync.Mutex
		server   *exec.Cmd
		stop     = make(chan struct{})
		reload   = make(chan struct{}, 1)
		signals  = make(chan os.Signal, 1)
		shutdown = make(chan struct{})
	)

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
		if dir := DevServerBinDir(); dir != "" {
			_ = os.RemoveAll(dir)
		}
	}

	since := time.Now()
	before := ScanModTimes(dir)

	if !buildAndServe(dir, pkg, resolvedAddr, runtimeIsolation, &mu, &server, noA11y, hold, shutdown) && !closed(shutdown) {
		cli.Fail("Initial build failed. Fixing and saving will retry")
	}
	if closed(shutdown) {
		exit()
		return
	}

	watch := NewWatch(watchCtx, dir, pkg, nil)
	go watch.Notify(before, since, stop, reload)

	contractWatch := NewDevContractWatch(dir)

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
				go watch.Refresh()
				contractWatch.Run()
			} else if !closed(shutdown) {
				cli.Fail("Build failed. Fixing and saving will retry")
			}
		}
	}
}

// ShutdownSignals end a dev loop cleanly: the app stopped, the hold released, the binary directory removed.
var ShutdownSignals = []os.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP}

func closed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

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

func addrInUse(err error) bool {
	var errno syscall.Errno
	return errors.Is(err, syscall.EADDRINUSE) || (errors.As(err, &errno) && errno == 10048)
}

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

func buildAndServe(dir, pkg, addr string, runtimeIsolation *isolation.Runtime, mu *sync.Mutex, cmd **exec.Cmd, noA11y bool, hold *devHold, interrupt <-chan struct{}) bool {
	out := &holdOutput{}
	if !buildGatesAndBinary(dir, pkg, runtimeIsolation, mu, cmd, noA11y, out, interrupt) {
		hold.show(holdBuildFailed, out.String())
		return false
	}
	tmpBin := DevServerBinaryPath(runtimeIsolation)

	childEnv := BuildDevChildEnv(runtimeIsolation.Env(os.Environ()), addr)
	runCmd := exec.Command(tmpBin, "--addr", addr)
	runCmd.Dir = dir
	runCmd.Stdout = os.Stdout
	runCmd.Stderr = os.Stderr
	runCmd.Env = childEnv
	TieToParent(runCmd)

	mu.Lock()
	*cmd = runCmd
	mu.Unlock()

	hold.release()
	if err := runCmd.Start(); err != nil {
		msg := fmt.Sprintf("Failed to start the app: %v", err)
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

	go func(stdout *os.File) {
		err := runCmd.Wait()
		if err != nil {
			fmt.Fprintf(stdout, "\n%s\n", cli.InfoString("Server exited"))
		}
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

func buildGatesAndBinary(dir, pkg string, runtimeIsolation *isolation.Runtime, mu *sync.Mutex, cmd **exec.Cmd, noA11y bool, out io.Writer, interrupt <-chan struct{}) bool {
	if !devA11yGate(dir, noA11y, out) {
		return false
	}
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
	buildCmd.Dir = dir
	buildCmd.Stdout = io.MultiWriter(os.Stdout, out)
	buildCmd.Stderr = io.MultiWriter(os.Stderr, out)
	buildCmd.WaitDelay = 5 * time.Second

	mu.Lock()
	*cmd = buildCmd
	mu.Unlock()

	return RunBuildWithHeartbeat(buildCmd, dir, interrupt) == nil
}
