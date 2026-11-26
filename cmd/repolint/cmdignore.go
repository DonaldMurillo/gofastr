package main

import (
	"os"
	"path/filepath"
	"strings"
)

func lintCommandBinariesIgnored(root string) ([]finding, error) {
	entries, err := os.ReadDir(filepath.Join(root, "cmd"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	body, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	ignored := map[string]bool{}
	for line := range strings.SplitSeq(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		ignored[strings.TrimPrefix(line, "/")] = true
	}
	var out []finding
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if ignored[name] {
			continue
		}
		if _, statErr := os.Stat(filepath.Join(root, name)); statErr == nil {
			continue
		}
		out = append(out, finding{
			File: ".gitignore",
			Rule: "cmd-binary-not-ignored",
			Message: "cmd/" + name + " builds to /" + name + " but .gitignore has no entry for it: " +
				"a `go build ./cmd/" + name + "` from the root leaves an untracked binary",
		})
	}
	return out, nil
}
