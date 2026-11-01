// Package context implements project-instruction file readers.
//
// Per § Skills (SKILL.md) and context (AGENTS.md), the harness reads
// project context from a profile-configured list of (path, label)
// tuples. AGENTS.md is the open standard primary surface; vendor
// fallback files (CLAUDE.md, .cursorrules, GEMINI.md, …) are read if
// present.
//
// Each entry in the profile's context_sources list is resolved to a
// Reader by Resolve. The result is fed into the engine's request
// middleware chain via ContextSection wrapped in
// <untrusted-NAME>...</untrusted-NAME> tags (rule 12).
package context

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Section is one chunk of project context read from disk.
type Section struct {
	Source string // canonical name (e.g. "agents-md", "claude-md")
	Path   string // absolute path the content came from
	Body   string // file content, post-newline-normalization
	SHA256 string // for TOFU comparison
}

// Reader reads project context from a working directory.
type Reader struct {
	// WorkingDir is the root the reader walks from. AGENTS.md
	// walks upward from this to the repo root.
	WorkingDir string

	// Sources is the configured list, names like "AGENTS.md" or
	// "CLAUDE.md". Per § Profiles, this is just a strings list, not
	// a polymorphism point.
	Sources []string
}

// Read returns all sections found, in profile-configured order. Each
// AGENTS.md walk produces one section per file encountered (deepest
// first → root last, so child rules apply atop parent rules).
//
// Missing files are silently skipped, readers don't error on the
// happy "no AGENTS.md in this repo" path. So are files the reader
// refuses: a symlink that leaves the file's directory, anything that
// is not a regular file, and a file over maxContextFileBytes.
func (r *Reader) Read() ([]Section, error) {
	if r.WorkingDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		r.WorkingDir = wd
	}
	abs, err := filepath.Abs(r.WorkingDir)
	if err != nil {
		return nil, err
	}
	var sections []Section
	for _, src := range r.Sources {
		switch src {
		case "AGENTS.md":
			files, err := walkUpward(abs, "AGENTS.md")
			if err != nil {
				return nil, err
			}
			for _, f := range files {
				body, sum, err := readFile(filepath.Dir(f), "AGENTS.md")
				if err != nil {
					// A refused file (a symlink out of its directory,
					// a FIFO or device, an oversized file) is skipped
					// like a missing one: one bad file must not drop
					// the rest of the context.
					continue
				}
				sections = append(sections, Section{
					Source: "agents-md",
					Path:   f,
					Body:   body,
					SHA256: sum,
				})
			}
		case "CLAUDE.md", ".cursorrules", "GEMINI.md", ".windsurfrules":
			s, ok := readSingle(abs, src, canonicalName(src))
			if ok {
				sections = append(sections, s)
			}
		case ".github/copilot-instructions.md":
			s, ok := readSingle(abs, src, "copilot-instructions")
			if ok {
				sections = append(sections, s)
			}
		default:
			return nil, fmt.Errorf("context: unknown source %q (allowed: AGENTS.md, CLAUDE.md, .cursorrules, GEMINI.md, .windsurfrules, .github/copilot-instructions.md)", src)
		}
	}
	return sections, nil
}

// walkUpward returns AGENTS.md files from the deepest directory
// (workingDir) up to the repo root, in deepest-first order so
// concatenated content reads "child overrides parent."
func walkUpward(workingDir, basename string) ([]string, error) {
	var out []string
	dir := workingDir
	for {
		p := filepath.Join(dir, basename)
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			out = append(out, p)
		}
		// Repo-root sentinel: stop at the filesystem root or when
		// .git appears (avoid walking up out of the repo into $HOME).
		if dir == filepath.Dir(dir) {
			break
		}
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			break
		}
		dir = filepath.Dir(dir)
	}
	return out, nil
}

func readSingle(workingDir, rel, sourceName string) (Section, bool) {
	p := filepath.Join(workingDir, rel)
	body, sum, err := readFile(workingDir, rel)
	if err != nil {
		return Section{}, false
	}
	return Section{Source: sourceName, Path: p, Body: body, SHA256: sum}, true
}

// maxContextFileBytes caps one context file. The body goes into every
// system prompt, and an unbounded read of a /dev/zero symlink would
// exhaust memory.
const maxContextFileBytes = 256 << 10

// errNotRegular refuses a context path that is not a regular file.
var errNotRegular = errors.New("context: not a regular file")

// readFile reads name through an *os.Root over dir, so a context file
// that is a symlink pointing out of dir (a repo's AGENTS.md ->
// ~/.ssh/id_ed25519, or a .github directory linked elsewhere) is
// refused instead of sent to the provider. Only regular files are read, checked before the open
// so a FIFO cannot block it, and the read stops at maxContextFileBytes.
func readFile(dir, name string) (string, string, error) {
	data, err := readRooted(dir, name)
	if err != nil {
		return "", "", err
	}
	body := normalizeNewlines(string(data))
	sum := sha256Hex(data)
	return body, sum, nil
}

func readRooted(dir, name string) ([]byte, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	info, err := root.Stat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: %w", name, errNotRegular)
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	// Check the handle too: the path may have been swapped between the
	// stat and the open.
	if info, err = f.Stat(); err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: %w", name, errNotRegular)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxContextFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxContextFileBytes {
		return nil, fmt.Errorf("context: %s is over %d bytes", name, maxContextFileBytes)
	}
	return data, nil
}

func normalizeNewlines(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
}

func canonicalName(filename string) string {
	switch filename {
	case "CLAUDE.md":
		return "claude-md"
	case ".cursorrules":
		return "cursorrules"
	case "GEMINI.md":
		return "gemini-md"
	case ".windsurfrules":
		return "windsurfrules"
	}
	return strings.ToLower(strings.TrimPrefix(filename, "."))
}

// ErrNoSourcesConfigured is returned when Reader.Sources is empty.
var ErrNoSourcesConfigured = errors.New("context: no sources configured")
