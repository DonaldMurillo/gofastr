package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
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
	}

	runtimeIsolation, resolvedAddr, err := resolveDevIsolation(dir, addr)
	if err != nil {
		fail("Isolation failed: %v", err)
		osExit(1)
	}

	fmt.Printf("\n  %s Dev server with hot reload\n\n", bold("GoFastr"))
	info("Watching %s for changes (.go, .js, .css, .html)...", dir)
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
	if !buildAndServe(dir, resolvedAddr, runtimeIsolation, &mu, &server) {
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
			close(stop)
			return

		case <-reload:
			fmt.Println()
			info("Change detected — rebuilding...")
			killServer(&mu, &server)
			if buildAndServe(dir, resolvedAddr, runtimeIsolation, &mu, &server) {
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

// buildAndServe builds and starts the server process.
func buildAndServe(dir, addr string, runtimeIsolation *isolation.Runtime, mu *sync.Mutex, cmd **exec.Cmd) bool {
	// Build binary to temp file
	tmpName := "gofastr-dev-server"
	if runtimeIsolation.Active() {
		tmpName += "-" + runtimeIsolation.ID()
	}
	tmpBin := filepath.Join(os.TempDir(), tmpName)
	buildCmd := exec.Command("go", "build", "-o", tmpBin, ".")
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

	// Wait for it in background so we can detect crashes
	go func() {
		if err := runCmd.Wait(); err != nil {
			fmt.Println()
			info("Server exited")
		}
	}()

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
// source and embedded-asset files. Go embeds .js, .css, and .html at
// build time, so changes to those files also require a rebuild.
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
		case ".go", ".js", ".css", ".html":
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
