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
	// WorkingDir is where reading starts; AGENTS.md is also looked up in each parent up to the repo root.
	WorkingDir string
	// Sources names the files to read, such as "AGENTS.md" or "CLAUDE.md".
	Sources []string
}

// Read returns the sections found in Sources order, skipping missing and refused files; it errors on an unknown source.
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

func walkUpward(workingDir, basename string) ([]string, error) {
	var out []string
	dir := workingDir
	for {
		p := filepath.Join(dir, basename)
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			out = append(out, p)
		}
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

const maxContextFileBytes = 256 << 10

var errNotRegular = errors.New("context: not a regular file")

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

// ErrNoSourcesConfigured names an empty Reader.Sources; Read itself returns no sections and a nil error then.
var ErrNoSourcesConfigured = errors.New("context: no sources configured")
