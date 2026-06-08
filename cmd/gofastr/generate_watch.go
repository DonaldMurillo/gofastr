package main

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// runGenerateWatch polls the blueprint (or gofastr.codegen.yml inputs) for
// changes and re-runs generateProject whenever their content shifts. Polling
// rather than fsnotify so we stay dependency-free; debounced to ~250ms.
//
// Usage:
//
//	gofastr generate --watch [--from=gofastr.yml] [--out=...]
//
// Stop with Ctrl-C.
func runGenerateWatch(args []string) {
	options := parseGenerateOptions(args)
	info("Watching generator inputs — Ctrl-C to stop.")

	// Initial pass.
	runOnce(args)

	lastHash := hashGenerateInputs(options)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		h := hashGenerateInputs(options)
		if h == lastHash {
			continue
		}
		lastHash = h
		fmt.Printf("\033[2J\033[H") // clear terminal
		info("Detected codegen input change — regenerating...")
		runOnce(args)
	}
}

// runOnce invokes generateProject with the same args but stripping --watch
// so it doesn't recurse.
func runOnce(args []string) {
	filtered := make([]string, 0, len(args))
	for _, a := range args {
		if a == "--watch" {
			continue
		}
		filtered = append(filtered, a)
	}
	exe, err := os.Executable()
	if err != nil {
		fail("generate failed: %v", err)
		return
	}
	cmd := exec.Command(exe, append([]string{"generate"}, filtered...)...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		fail("generate failed: %v", err)
	}
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

func hashGenerateInputs(options generateOptions) string {
	h := sha256.New()
	if options.from != "" {
		hashBlueprintInputInto(h, options.from)
		return fmt.Sprintf("%x", h.Sum(nil))
	}
	if options.configPath != "" {
		hashFileInto(h, options.configPath)
	}
	discovery, err := discoverGenerateConfig(options)
	if err == nil && discovery.Found {
		projectDir := discovery.ProjectDir
		if strings.TrimSpace(projectDir) == "" {
			projectDir = "."
		}
		hashFileInto(h, discovery.Path)
		for _, ext := range discovery.Config.Codegen.Extensions {
			if len(ext.Command) > 0 && filepath.Base(ext.Command[0]) != ext.Command[0] {
				hashFileInto(h, projectRelativePath(projectDir, ext.Command[0]))
			}
		}
		// A gofastr.codegen.yml extension generator may still feed itself a
		// json_file/json_dir source via the general codegen framework; hash
		// those inputs so --watch reacts to them.
		for _, gen := range discovery.Config.Codegen.Generators {
			switch strings.ToLower(gen.Source.Type) {
			case "json_file":
				hashFileInto(h, projectRelativePath(projectDir, gen.Source.Path))
			case "json_dir":
				fmt.Fprintln(h, hashEntitiesDir(projectRelativePath(projectDir, gen.Source.Path)))
			}
		}
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func hashBlueprintInputInto(h interface{ Write([]byte) (int, error) }, path string) {
	info, err := os.Stat(path)
	if err != nil {
		fmt.Fprintln(h, path)
		return
	}
	if !info.IsDir() {
		hashFileInto(h, path)
		return
	}
	var paths []string
	_ = filepath.WalkDir(path, func(next string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !isBlueprintFile(next) {
			return nil
		}
		paths = append(paths, next)
		return nil
	})
	sort.Strings(paths)
	for _, next := range paths {
		hashFileInto(h, next)
	}
}

func hashFileInto(h interface{ Write([]byte) (int, error) }, path string) {
	fmt.Fprintln(h, path)
	data, err := os.ReadFile(path)
	if err == nil {
		_, _ = h.Write(data)
	}
}

func projectRelativePath(projectDir, path string) string {
	if filepath.IsAbs(path) || strings.TrimSpace(projectDir) == "" || projectDir == "." {
		return path
	}
	return filepath.Join(projectDir, path)
}
