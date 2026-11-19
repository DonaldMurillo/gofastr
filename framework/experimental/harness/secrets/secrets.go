// Package secrets loads provider API keys from a repository's .harness-secrets/env file
// (KEY=VALUE lines, # comments, optional quotes) into environment variables not already set.
package secrets

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LoadRepo is LoadFrom the working directory.
func LoadRepo() (string, error) {
	start, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return LoadFrom(start)
}

// LoadFrom walks up from dir to the repository root for .harness-secrets/env, loads it, and returns its path, or "" when there is none.
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

func findSecretsFile(start string) (string, bool) {
	dir := start
	for {
		candidate := filepath.Join(dir, ".harness-secrets", "env")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return "", false
		}
		dir = parent
	}
}

func loadFile(path string) error {
	pending, err := pendingFromFile(path)
	if err != nil {
		return err
	}
	return apply(pending)
}

type pair struct{ key, val string }

func apply(pending []pair) error {
	for _, p := range pending {
		if _, present := os.LookupEnv(p.key); present {
			continue
		}
		if err := os.Setenv(p.key, p.val); err != nil {
			return fmt.Errorf("secrets: setenv %q: %w", p.key, err)
		}
	}
	return nil
}

func pendingFromFile(path string) ([]pair, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var pending []pair
	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		before, after, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("secrets: %s:%d: missing '='", path, lineNo)
		}
		key := strings.TrimSpace(before)
		val := strings.TrimSpace(after)
		val = trimQuotes(val)
		if val == "" {
			continue
		}
		if !isProviderCredentialKey(key) {
			continue
		}
		if _, present := os.LookupEnv(key); present {
			continue
		}
		pending = append(pending, pair{key, val})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return pending, nil
}

func isProviderCredentialKey(key string) bool {
	if !isEnvVarName(key) {
		return false
	}
	if strings.HasPrefix(key, "GOFASTR_") {
		return false
	}
	return strings.HasSuffix(key, "_API_KEY") || strings.HasSuffix(key, "_TOKEN")
}

func isEnvVarName(k string) bool {
	if k == "" || k[0] < 'A' || k[0] > 'Z' {
		return false
	}
	for i := 1; i < len(k); i++ {
		c := k[i]
		if (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '_' {
			return false
		}
	}
	return true
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

// ErrInvalid reports a malformed secrets file.
var ErrInvalid = errors.New("secrets: invalid file format")
