package dotenv

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
)

// Load merges the .env files at paths, earlier paths winning, skipping missing files.
// A ${NAME} reads os.Environ first, then an earlier file, then the same file's earlier keys.
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
		parsed, err := parse(f, merged, os.LookupEnv)
		_ = f.Close()
		if err != nil {
			return nil, fmt.Errorf("dotenv: parse %s: %w", p, err)
		}
		for k, v := range parsed {
			if _, set := merged[k]; set {
				continue
			}
			merged[k] = v
		}
	}
	return merged, nil
}

// Apply sets each key of vals not already in the environment and returns the sorted keys it set and skipped.
func Apply(vals map[string]string) (loaded, skipped []string) {
	for k, v := range vals {
		if _, exists := os.LookupEnv(k); exists {
			skipped = append(skipped, k)
			continue
		}
		if err := os.Setenv(k, v); err == nil {
			loaded = append(loaded, k)
		}
	}
	sort.Strings(loaded)
	sort.Strings(skipped)
	return loaded, skipped
}

// LoadAndApply is Load followed by Apply.
func LoadAndApply(paths ...string) error {
	vals, err := Load(paths...)
	if err != nil {
		return err
	}
	_, _ = Apply(vals)
	return nil
}
