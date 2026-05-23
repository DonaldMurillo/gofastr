package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/isolation"
)

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
		os.Exit(1)
	}

	fmt.Printf("\n  %s Dev server with hot reload\n\n", bold("GoFastr"))
	info("Watching %s for *.go changes...", dir)
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

func resolveDevIsolation(dir, addr string) (isolation.Runtime, string, error) {
	runtimeIsolation, err := isolation.Resolve(dir)
	if err != nil {
		return isolation.Runtime{}, "", err
	}
	resolvedAddr, err := runtimeIsolation.Addr(addr)
	if err != nil {
		return isolation.Runtime{}, "", err
	}
	return runtimeIsolation, resolvedAddr, nil
}

// buildAndServe builds and starts the server process.
func buildAndServe(dir, addr string, runtimeIsolation isolation.Runtime, mu *sync.Mutex, cmd **exec.Cmd) bool {
	// Build binary to temp file
	tmpName := "gofastr-dev-server"
	if runtimeIsolation.Active() {
		tmpName += "-" + runtimeIsolation.ID()
	}
	tmpBin := filepath.Join(os.TempDir(), tmpName)
	buildCmd := exec.Command("go", "build", "-o", tmpBin, dir)
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr

	mu.Lock()
	*cmd = buildCmd
	mu.Unlock()

	if err := buildCmd.Run(); err != nil {
		return false
	}

	// Start the server
	runCmd := exec.Command(tmpBin, "--addr", addr)
	runCmd.Stdout = os.Stdout
	runCmd.Stderr = os.Stderr
	runCmd.Env = runtimeIsolation.Env(os.Environ())

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

// scanModTimes walks the directory and records the latest mod time of all .go files.
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
		if filepath.Ext(path) == ".go" {
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
