package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// storedConfig is what `meridian login` persists: the server URL and a
// scoped API token, at <user-config-dir>/meridian/config.json (0600).
type storedConfig struct {
	URL   string `json:"url,omitempty"`
	Token string `json:"token,omitempty"`
}

func configPath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, binaryName, "config.json"), nil
}

// loadConfig returns the stored config, or the zero value when there is
// none: a missing or unreadable file is not an error, it just means the
// caller falls through to flags/env. A CORRUPT file is refused loudly
// instead: the zero value would march on as data, silently dropping the
// stored token so every call runs unauthenticated against the
// operator's assumption.
func loadConfig() storedConfig {
	var cfg storedConfig
	path, err := configPath()
	if err != nil {
		return cfg
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		fmt.Fprintf(os.Stderr, "%s: config file %s is corrupt (%v); delete it or run '%s login' again\n", binaryName, path, err, binaryName)
		os.Exit(1)
	}
	return cfg
}

func saveConfig(cfg storedConfig) (string, error) {
	path, err := configPath()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	// 0600 on CREATE and OVERWRITE alike: os.WriteFile applies its mode
	// only when creating, so a pre-existing 0644 config.json (operator
	// chmod, restored backup, dotfiles manager) would be truncated and
	// refilled with the bearer token while still world-readable. Open,
	// chmod the handle, then write — the credential bytes land only
	// after the mode is fixed.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return "", err
	}
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return "", err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return "", err
	}
	return path, f.Close()
}
