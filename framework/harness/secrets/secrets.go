// Package secrets locates and loads the repo-local
// .harness-secrets/env file. The file format is a tiny KEY=VALUE
// subset of dotenv: one assignment per line, `#` for comments,
// optional surrounding quotes on values.
//
// The package walks upward from a starting directory until it finds
// .harness-secrets/env, so it works whether tests run from the
// module root or from a subpackage.
//
// Env vars already set in the process take priority — the file is a
// fallback, not an override.
package secrets

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LoadRepo finds and loads .harness-secrets/env. Returns the path it
// loaded from (or empty if none found). Missing file is not an error.
func LoadRepo() (string, error) {
	start, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return LoadFrom(start)
}

// LoadFrom walks upward from `dir` until it finds
// .harness-secrets/env. Returns the path loaded (or "" if missing).
func LoadFrom(dir string) (string, error) {
	path, ok := findSecretsFile(dir)
	if !ok {
		return "", nil
	}
	if err := loadFile(path); err != nil {
		return path, err
	}
	return path, nil
}

// findSecretsFile walks upward looking for .harness-secrets/env.
// Stops at the filesystem root or when a .git directory is seen.
func findSecretsFile(start string) (string, bool) {
	dir := start
	for {
		candidate := filepath.Join(dir, ".harness-secrets", "env")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, true
		}
		// Stop at filesystem root.
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		// Stop at repo root (.git dir signals it).
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return "", false
		}
		dir = parent
	}
}

func loadFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			return fmt.Errorf("secrets: %s:%d: missing '='", path, lineNo)
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.TrimSpace(line[eq+1:])
		val = trimQuotes(val)
		if val == "" {
			continue // empty value → don't set; lets shell env stay authoritative
		}
		// Env vars already set in the process take priority.
		if _, present := os.LookupEnv(key); present {
			continue
		}
		if err := os.Setenv(key, val); err != nil {
			return fmt.Errorf("secrets: setenv %q: %w", key, err)
		}
	}
	return scanner.Err()
}

func trimQuotes(s string) string {
	if len(s) >= 2 {
		first, last := s[0], s[len(s)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// ErrInvalid is returned when the secrets file is malformed.
var ErrInvalid = errors.New("secrets: invalid file format")
