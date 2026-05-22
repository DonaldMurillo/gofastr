package codegen

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// LoadSource loads the source configured for a generator.
func LoadSource(projectDir string, source SourceConfig) (any, error) {
	switch strings.ToLower(strings.TrimSpace(source.Type)) {
	case "":
		return nil, nil
	case "json_file":
		path, err := safeProjectPath(projectDir, source.Path)
		if err != nil {
			return nil, err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var out any
		if err := json.Unmarshal(data, &out); err != nil {
			return nil, fmt.Errorf("parse %s: %w", source.Path, err)
		}
		return out, nil
	case "json_dir":
		dir, err := safeProjectPath(projectDir, source.Path)
		if err != nil {
			return nil, err
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, err
		}
		var names []string
		for _, entry := range entries {
			if entry.IsDir() || strings.ToLower(filepath.Ext(entry.Name())) != ".json" {
				continue
			}
			names = append(names, entry.Name())
		}
		sort.Strings(names)
		out := make([]JSONDocument, 0, len(names))
		for _, name := range names {
			rel := filepath.ToSlash(filepath.Join(source.Path, name))
			path, err := safeProjectPath(projectDir, rel)
			if err != nil {
				return nil, err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			var value any
			if err := json.Unmarshal(data, &value); err != nil {
				return nil, fmt.Errorf("parse %s: %w", rel, err)
			}
			out = append(out, JSONDocument{Path: rel, Data: value})
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported source type %q", source.Type)
	}
}

func safeProjectPath(projectDir, rel string) (string, error) {
	if strings.TrimSpace(rel) == "" {
		return "", fmt.Errorf("source path must not be empty")
	}
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("source path %q must be relative", rel)
	}
	clean := filepath.Clean(rel)
	for _, part := range strings.Split(filepath.ToSlash(clean), "/") {
		if part == ".." {
			return "", fmt.Errorf("source path %q must not contain parent traversal", rel)
		}
	}
	root, err := filepath.Abs(projectDir)
	if err != nil {
		return "", err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	path := filepath.Join(root, clean)
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	relToRoot, err := filepath.Rel(root, realPath)
	if err != nil {
		return "", err
	}
	if relToRoot == ".." || strings.HasPrefix(filepath.ToSlash(relToRoot), "../") || filepath.IsAbs(relToRoot) {
		return "", fmt.Errorf("source path %q resolves outside the project", rel)
	}
	return realPath, nil
}
