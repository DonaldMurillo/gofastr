package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

var testOnlyModules = []string{
	"github.com/testcontainers/testcontainers-go",
}

func lintConsumerModuleGraph(root string) ([]finding, error) {
	body, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []finding
	inRequireBlock := false
	for i, line := range strings.Split(string(body), "\n") {
		code, _, _ := strings.Cut(line, "//")
		trimmed := strings.TrimSpace(code)
		if trimmed == "" {
			continue
		}
		if directive, ok := strings.CutSuffix(trimmed, "("); ok {
			inRequireBlock = strings.TrimSpace(directive) == "require"
			continue
		}
		if trimmed == ")" {
			inRequireBlock = false
			continue
		}
		fields := strings.Fields(trimmed)
		if !inRequireBlock {
			if len(fields) < 2 || fields[0] != "require" {
				continue
			}
			fields = fields[1:]
		}
		path := fields[0]
		if unq, err := strconv.Unquote(path); err == nil {
			path = unq
		}
		for _, mod := range testOnlyModules {
			if path != mod && !strings.HasPrefix(path, mod+"/") {
				continue
			}
			out = append(out, finding{
				File:    "go.mod",
				Line:    i + 1,
				Rule:    "test-only-dep-in-consumer-graph",
				Message: mod + " is test-only; a require here is inherited by every app that imports GoFastr. Supply it through TEST_POSTGRES_DSN in CI, or move it to a nested module",
			})
		}
	}
	return out, nil
}
