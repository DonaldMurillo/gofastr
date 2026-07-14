package evalrunner

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func resolveCodexHome(source string) (string, error) {
	if source == "" {
		if configured := strings.TrimSpace(os.Getenv("CODEX_HOME")); configured != "" {
			source = configured
		} else {
			userHome, err := os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("locate user home: %w", err)
			}
			source = filepath.Join(userHome, ".codex")
		}
	}
	home, err := filepath.Abs(source)
	if err != nil {
		return "", fmt.Errorf("resolve Codex home: %w", err)
	}
	authPath := filepath.Join(home, "auth.json")
	if _, err := os.Stat(authPath); err != nil {
		return "", fmt.Errorf("Codex authentication not found at %s: %w", authPath, err)
	}
	return filepath.Clean(home), nil
}

// codexEnvironment removes the parent Codex Desktop task identity and policy.
// The child gets only the selected authenticated home, then starts a brand-new
// ephemeral exec session; no parent task is resumed or named.
func codexEnvironment(home string) []string {
	env := filteredAgentEnvironment("codex")
	env = append(env, "CODEX_HOME="+home)
	sort.Strings(env)
	return env
}
