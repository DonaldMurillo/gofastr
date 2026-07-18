package main

import (
	"encoding/json"
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
// none — a missing or unreadable file is not an error, it just means the
// caller falls through to flags/env.
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
	_ = json.Unmarshal(data, &cfg)
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
	// 0600: the file holds a bearer credential.
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}
