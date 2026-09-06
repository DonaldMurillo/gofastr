// Package secrets locates and loads the repo-local
// .harness-secrets/env file. The file format is a tiny KEY=VALUE
// subset of dotenv: one assignment per line, `#` for comments,
// optional surrounding quotes on values.
//
// The package walks upward from a starting directory until it finds
// .harness-secrets/env, so it works whether tests run from the
// module root or from a subpackage.
//
// The file may only deliver PROVIDER CREDENTIALS: keys ending in
// _API_KEY or _TOKEN (never GOFASTR_*). A cloned repo's secrets file
// is attacker-authored, so anything that shapes the process — proxy
// selection, PATH, LD_PRELOAD, HOME — or that derives the credential
// store's own key is refused. Env vars already set in the process take
// priority; the file is a fallback, not an override.
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
		before, after, ok := strings.Cut(line, "=")
		if !ok {
			return fmt.Errorf("secrets: %s:%d: missing '='", path, lineNo)
		}
		key := strings.TrimSpace(before)
		val := strings.TrimSpace(after)
		val = trimQuotes(val)
		if val == "" {
			continue // empty value → don't set; lets shell env stay authoritative
		}
		// The file is found by walking UP from the working directory, so
		// on a cloned repo it is attacker-authored. Delivering provider
		// API keys from it is the documented contract; that is ALL it
		// may deliver. A positive allow-list, not a deny-list: anything
		// that shapes the process — proxy selection (HTTPS_PROXY,
		// all_proxy route every provider request, each carrying the
		// operator's real key, through a host the repo author chose),
		// PATH/LD_PRELOAD (binary and library resolution at the next
		// exec), HOME and XDG_* (where the credential store lives) — is
		// one forgotten deny entry away from planting, and
		// GOFASTR_HARNESS_* key material must never come from it either
		// (a planted MACHINE_KEY/PASSPHRASE seals the operator's first
		// stored credential under an attacker-known key). Only keys that
		// name a provider credential (suffix _API_KEY or _TOKEN) pass;
		// the real environment stays authoritative for everything else,
		// as it already is for any key the process has set.
		if !isProviderCredentialKey(key) {
			continue
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

// isProviderCredentialKey reports whether key names a provider
// credential — the only thing a walked secrets file may deliver. Keys
// ending in _API_KEY or _TOKEN qualify (ZAI_API_KEY,
// OPENROUTER_API_KEY, GITHUB_TOKEN, ...); anything else, including
// process-shaping names and every GOFASTR_HARNESS_* variable (the
// credential store's own key material), does not. The real environment
// stays authoritative for everything the file may not set.
func isProviderCredentialKey(key string) bool {
	if strings.HasPrefix(key, "GOFASTR_") {
		return false
	}
	return strings.HasSuffix(key, "_API_KEY") || strings.HasSuffix(key, "_TOKEN")
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
