package evalrunner

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func WorkspaceMetrics(root string) (sourceLines, testLines, directDeps int) {
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		file, openErr := os.Open(path)
		if openErr != nil {
			return nil
		}
		lines := 0
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			if strings.TrimSpace(scanner.Text()) != "" {
				lines++
			}
		}
		if err := scanner.Err(); err != nil {
			_ = file.Close()
			return fmt.Errorf("%s: %w", path, err)
		}
		_ = file.Close()
		if strings.HasSuffix(path, "_test.go") {
			testLines += lines
		} else {
			sourceLines += lines
		}
		return nil
	})
	goMod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return sourceLines, testLines, 0
	}
	inBlock := false
	for line := range strings.SplitSeq(string(goMod), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "require (":
			inBlock = true
		case inBlock && line == ")":
			inBlock = false
		case inBlock && line != "" && !strings.Contains(line, "// indirect"):
			directDeps++
		case strings.HasPrefix(line, "require ") && !strings.Contains(line, "// indirect"):
			directDeps++
		}
	}
	return sourceLines, testLines, directDeps
}
