// Package secrets locates and loads the repo-local
// .harness-secrets/env file. The file format is a tiny KEY=VALUE
// subset of dotenv: one assignment per line, `#` for comments,
// optional surrounding quotes on values.
//
// The package walks upward from a starting directory until it finds
// .harness-secrets/env, so it works whether tests run from the
// module root or from a subpackage.
//
// Env vars already set in the process take priority, the file is a
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
		// API keys from it is the documented contract; deciding how the
		// credential store is ENCRYPTED is not. A planted
		// GOFASTR_HARNESS_MACHINE_KEY or _PASSPHRASE means the operator's
		// first stored credential is sealed under a key the repo author
		// chose. Project hooks face the same untrusted-directory threat
		// and are off by default; this loader has no such gate, so the
		// key-derivation vars simply cannot come from the file. The real
		// environment stays authoritative for them, as it already is for
		// everything else here.
		if isHarnessKeyVar(key) {
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

// harnessKeyVars are the variables that decide how the credential store
// is encrypted. They are refused from a walked secrets file.
var harnessKeyVars = map[string]bool{
	"GOFASTR_HARNESS_MACHINE_KEY": true,
	"GOFASTR_HARNESS_PASSPHRASE":  true,
}

// isHarnessKeyVar reports whether key selects credential-store key
// material rather than a provider credential.
func isHarnessKeyVar(key string) bool { return harnessKeyVars[key] }

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
