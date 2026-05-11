package main

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// runGenerateWatch polls the entities directory for changes and re-runs
// generateProject whenever a *.json file's content shifts. Polling rather
// than fsnotify so we stay dependency-free; debounced to ~200ms.
//
// Usage:
//
//	gofastr generate --watch [--entities=...] [--out=...]
//
// Stop with Ctrl-C.
func runGenerateWatch(args []string) {
	options := parseGenerateOptions(args)
	info("Watching %s for changes — Ctrl-C to stop.", options.entitiesDir)

	// Initial pass.
	runOnce(args, options.entitiesDir)

	lastHash := hashEntitiesDir(options.entitiesDir)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		h := hashEntitiesDir(options.entitiesDir)
		if h == lastHash {
			continue
		}
		lastHash = h
		fmt.Printf("\033[2J\033[H") // clear terminal
		info("Detected change in %s — regenerating...", options.entitiesDir)
		runOnce(args, options.entitiesDir)
	}
}

// runOnce invokes generateProject with the same args but stripping --watch
// so it doesn't recurse.
func runOnce(args []string, _ string) {
	filtered := make([]string, 0, len(args))
	for _, a := range args {
		if a == "--watch" {
			continue
		}
		filtered = append(filtered, a)
	}
	// generateProject calls os.Exit on failure; defer-recover so a single
	// bad file doesn't kill the watcher.
	defer func() {
		if r := recover(); r != nil {
			fail("recovered from generate panic: %v", r)
		}
	}()
	generateProject(filtered)
}

// hashEntitiesDir returns a content hash over every *.json file in dir,
// sorted by name. Used to detect "did anything change since last tick".
// Missing directory or read errors collapse to an empty hash (the watch
// silently waits for the directory to appear / become readable).
func hashEntitiesDir(dir string) string {
	var paths []string
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".json" {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	sort.Strings(paths)

	h := sha256.New()
	for _, p := range paths {
		fmt.Fprintln(h, p)
		buf, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		h.Write(buf)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}
