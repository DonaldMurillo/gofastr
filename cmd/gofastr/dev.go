package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/isolation"
)

// buildDevChildEnv produces the env slice handed to the rebuilt server
// process. It drops every pre-existing GOFASTR_DEV entry and prepends
// GOFASTR_DEV=1 — necessary because:
//
//   - macOS getenv returns the LAST occurrence; appending wins.
//   - Linux glibc getenv returns the FIRST occurrence; a parent
//     GOFASTR_DEV=0 would silently defeat the override.
//
// Dropping duplicates and prepending makes the override platform-
// independent and immune to a user's prior export.
func buildDevChildEnv(parent []string) []string {
	out := make([]string, 0, len(parent)+1)
	out = append(out, "GOFASTR_DEV=1")
	for _, kv := range parent {
		if strings.HasPrefix(kv, "GOFASTR_DEV=") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

func runDev(args []string) {
	addr := "localhost:8080"
	dir := "."
	// pkg is the package to build, relative to dir. It defaults to dir itself,
	// which is right for the scaffold layout (main at the project root). Apps
	// that keep main under cmd/<name>/ need the two to differ: the build target
	// is the command, but the watch root and the server's cwd must stay at the
	// project root — otherwise the watcher misses internal/ and relative paths
	// (sqlite db_url, static dirs) resolve against the command dir instead.
	pkg := "."
	noA11y := false
	for i, a := range args {
		if a == "--addr" && i+1 < len(args) {
			addr = args[i+1]
		}
		if a == "-p" && i+1 < len(args) {
			addr = "localhost:" + args[i+1]
		}
		if a == "--dir" && i+1 < len(args) {
			dir = args[i+1]
		}
		if a == "--pkg" && i+1 < len(args) {
			pkg = args[i+1]
		}
		if a == "--no-a11y" {
			noA11y = true
		}
	}

	runtimeIsolation, resolvedAddr, err := resolveDevIsolation(dir, addr)
	if err != nil {
		fail("Isolation failed: %v", err)
		osExit(1)
	}

	fmt.Printf("\n  %s Dev server with hot reload\n\n", bold("GoFastr"))
	info("Watching %s for changes (.go, .js, .css, .html, .md)...", dir)
	if pkg != "." {
		info("Building %s", pkg)
	}
	if runtimeIsolation.Active() && resolvedAddr != addr {
		info("Isolation %s remapped http://%s -> http://%s", runtimeIsolation.ID(), addr, resolvedAddr)
	} else {
		info("Server at http://%s", resolvedAddr)
	}
	fmt.Println()

	var (
		mu       sync.Mutex
		server   *exec.Cmd
		stop     = make(chan struct{})
		reload   = make(chan struct{}, 1)
		shutdown = make(chan os.Signal, 1)
	)

	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)

	// Build and start the server initially
	if !buildAndServe(dir, pkg, resolvedAddr, runtimeIsolation, &mu, &server, noA11y) {
		fail("Initial build failed — fixing and saving will retry")
	}

	// File watcher goroutine — polls for .go file changes
	go func() {
		prev := scanModTimes(dir)
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				curr := scanModTimes(dir)
				if changed(prev, curr) {
					prev = curr
					select {
					case reload <- struct{}{}:
					default: // already queued
					}
				}
			}
		}
	}()

	// Main loop: wait for reload or shutdown
	for {
		select {
		case <-shutdown:
			fmt.Println()
			info("Shutting down...")
			killServer(&mu, &server)
			_ = os.Remove(devServerBinaryPath(runtimeIsolation))
			close(stop)
			return

		case <-reload:
			fmt.Println()
			info("Change detected — rebuilding...")
			killServer(&mu, &server)
			if buildAndServe(dir, pkg, resolvedAddr, runtimeIsolation, &mu, &server, noA11y) {
				success("Reloaded!")
			} else {
				fail("Build failed — fixing and saving will retry")
			}
		}
	}
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

// devServerBinaryPath is the per-process temp path the rebuilt server is
// compiled to. The pid suffix lets concurrent dev instances coexist; the
// shutdown path removes the file so restarts don't accumulate binaries in
// the temp dir.
func devServerBinaryPath(runtimeIsolation *isolation.Runtime) string {
	tmpName := fmt.Sprintf("gofastr-dev-server-%d", os.Getpid())
	if runtimeIsolation.Active() {
		tmpName += "-" + runtimeIsolation.ID()
	}
	if runtime.GOOS == "windows" {
		tmpName += ".exe"
	}
	return filepath.Join(os.TempDir(), tmpName)
}

// devA11yGate runs the same static accessibility lint `gofastr build`
// enforces, against the dev watch root. Kept as its own decision func so
// the skip semantics are testable without compiling a project.
func devA11yGate(dir string, noA11y bool) bool {
	if noA11y {
		return true
	}
	if !buildA11yGate(dir) {
		fail("Accessibility lint failed — fix the findings above (guided), or run with --no-a11y")
		return false
	}
	return true
}

// buildAndServe builds and starts the server process.
func buildAndServe(dir, pkg, addr string, runtimeIsolation *isolation.Runtime, mu *sync.Mutex, cmd **exec.Cmd, noA11y bool) bool {
	// Same posture as `gofastr build`: the static a11y lint gates the
	// rebuild by default. Failing here is a build failure — the watch
	// loop keeps running and the next save retries.
	if !devA11yGate(dir, noA11y) {
		return false
	}

	// Build binary to temp file. pkg is relative to dir ("." unless --pkg moved
	// the build target to a command under cmd/), so the build resolves against
	// the project module while the watch root and cwd below stay at dir.
	tmpBin := devServerBinaryPath(runtimeIsolation)
	buildCmd := exec.Command("go", "build", "-o", tmpBin, pkg)
	buildCmd.Dir = dir // Run from the project dir so go build resolves the local module.
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr

	mu.Lock()
	*cmd = buildCmd
	mu.Unlock()

	if err := buildCmd.Run(); err != nil {
		return false
	}

	// Start the server. GOFASTR_DEV=1 signals to framework.NewApp +
	// uihost.New that this process is under `gofastr dev`, so they
	// auto-wire the livereload SSE endpoint and client script. The
	// host doesn't need any code change to get browser reload — and
	// production deployments don't accidentally serve it because
	// GOFASTR_ENV=production is checked as a kill switch.
	childEnv := buildDevChildEnv(runtimeIsolation.Env(os.Environ()))
	runCmd := exec.Command(tmpBin, "--addr", addr)
	// Run the server in the project dir — the same cwd it gets when run by
	// hand — so relative paths (sqlite db_url, static dir) resolve against
	// the project, and the app's own worktree-isolation lookup sees the
	// project's location rather than wherever `gofastr dev` was launched.
	runCmd.Dir = dir
	runCmd.Stdout = os.Stdout
	runCmd.Stderr = os.Stderr
	runCmd.Env = childEnv

	mu.Lock()
	*cmd = runCmd
	mu.Unlock()

	if err := runCmd.Start(); err != nil {
		fail("Failed to start: %v", err)
		return false
	}

	// Wait for it in background so we can detect crashes. The writer is
	// captured at spawn: this goroutine can outlive the caller, and the
	// coverage tests swap os.Stdout around buildAndServe — reading the
	// global here later would race that restore.
	go func(stdout *os.File) {
		if err := runCmd.Wait(); err != nil {
			fmt.Fprintf(stdout, "\n%s\n", infoString("Server exited"))
		}
	}(os.Stdout)

	return true
}

// killServer kills the current server process.
func killServer(mu *sync.Mutex, cmd **exec.Cmd) {
	mu.Lock()
	defer mu.Unlock()

	if *cmd != nil && (*cmd).Process != nil {
		(*cmd).Process.Signal(syscall.SIGTERM)
		time.Sleep(100 * time.Millisecond)
		(*cmd).Process.Kill()
		*cmd = nil
	}
}

// scanModTimes walks the directory and records the latest mod time of
// source and embedded-asset files. Go embeds .js, .css, .html, and .md
// at build time (framework docs, llm.md sources), so changes to those
// files also require a rebuild.
func scanModTimes(dir string) map[string]time.Time {
	result := make(map[string]time.Time)
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		// Skip vendor, .git, node_modules, tmp dirs
		name := info.Name()
		if info.IsDir() && (name == "vendor" || name == ".git" || name == "node_modules" || name == "tmp") {
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

// changed compares two mod-time maps and returns true if any file was added, removed, or modified.
func changed(prev, curr map[string]time.Time) bool {
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
