package evalrunner

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var tokenUsagePattern = regexp.MustCompile(`(?m)tokens used\r?\n([\d,]+)`)

func ParseTokenUsage(log []byte) int64 {
	matches := tokenUsagePattern.FindAllSubmatch(log, -1)
	if len(matches) == 0 {
		return 0
	}
	raw := strings.ReplaceAll(string(matches[len(matches)-1][1]), ",", "")
	value, _ := strconv.ParseInt(raw, 10, 64)
	return value
}

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
