package dotenv

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
)

// Load reads one or more .env files and returns the merged key/value
// map. EARLIER paths take precedence on key conflict — the convention
// is to pass most-specific first (e.g. .env.local, .env.<APP_ENV>,
// .env). Missing files are silently skipped; a malformed file returns
// an error naming the file.
func Load(paths ...string) (map[string]string, error) {
	merged := map[string]string{}
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("dotenv: open %s: %w", p, err)
		}
		parsed, err := Parse(f)
		_ = f.Close()
		if err != nil {
			return nil, fmt.Errorf("dotenv: parse %s: %w", p, err)
		}
		for k, v := range parsed {
			if _, set := merged[k]; set {
				// Earlier file already set this — keep its value.
				continue
			}
			merged[k] = v
		}
	}
	return merged, nil
}

// Apply writes vals into the process environment via os.Setenv, but
// ONLY for keys not already present in os.Environ. Returns the keys
// actually set (loaded) and those skipped because env already had a
// value. Both slices are sorted for stable test output.
//
// Existing env wins — operators expect their explicit configuration
// to override dotfiles. (Some loaders flip this; do not.)
func Apply(vals map[string]string) (loaded, skipped []string) {
	for k, v := range vals {
		if _, exists := os.LookupEnv(k); exists {
			skipped = append(skipped, k)
			continue
		}
		// Setenv error path is only platform-edge — empty key, NUL —
		// our parser already filters those. Swallow defensively
		// rather than fail the whole startup on a typo.
		if err := os.Setenv(k, v); err == nil {
			loaded = append(loaded, k)
		}
	}
	sort.Strings(loaded)
	sort.Strings(skipped)
	return loaded, skipped
}

// LoadAndApply is Load + Apply in one call — the common shape for
// boot-time wiring.
func LoadAndApply(paths ...string) error {
	vals, err := Load(paths...)
	if err != nil {
		return err
	}
	_, _ = Apply(vals)
	return nil
}
