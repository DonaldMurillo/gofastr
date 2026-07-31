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

	// Sources is the configured list — names like "AGENTS.md" or
	// "CLAUDE.md". Per § Profiles, this is just a strings list, not
	// a polymorphism point.
	Sources []string
}

// Read returns all sections found, in profile-configured order. Each
// AGENTS.md walk produces one section per file encountered (deepest
// first → root last, so child rules apply atop parent rules).
//
// Missing files are silently skipped — readers don't error on the
// happy "no AGENTS.md in this repo" path.
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
				body, sum, err := readFile(f)
				if err != nil {
					return nil, err
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
	body, sum, err := readFile(p)
	if err != nil {
		return Section{}, false
	}
	return Section{Source: sourceName, Path: p, Body: body, SHA256: sum}, true
}

func readFile(path string) (string, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", err
	}
	body := normalizeNewlines(string(data))
	sum := sha256Hex(data)
	return body, sum, nil
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
