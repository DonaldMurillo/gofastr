package clitest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func WriteReportExtension(t *testing.T, dir, marker string) string {
	t.Helper()
	var command []string
	if runtime.GOOS == "windows" {
		path := filepath.Join(dir, "report-extension.cmd")
		body := "@echo off\r\nmore >nul\r\n"
		if marker != "" {
			body += "type nul > \"" + marker + "\"\r\n"
		}
		body += "echo {\"files\":[{\"path\":\"report.go\",\"content\":\"package reports\\n\"}]}\r\n"
		WriteTestFile(t, path, body)
		command = []string{"cmd.exe", "/d", "/c", path}
	} else {
		path := filepath.Join(dir, "report-extension.sh")
		body := "#!/bin/sh\ncat >/dev/null\n"
		if marker != "" {
			body += "touch \"" + marker + "\"\n"
		}
		body += "printf '%s' '{\"files\":[{\"path\":\"report.go\",\"content\":\"package reports\\n\"}]}'\n"
		WriteTestFile(t, path, body)
		if err := os.Chmod(path, 0o755); err != nil {
			t.Fatal(err)
		}
		command = []string{path}
	}
	encoded, err := json.Marshal(command)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

// CopyGoSum copies the repo's go.sum into the temp module so `go test`
// inside the temp dir can satisfy transitive dependencies without needing
// network access.
func CopyGoSum(repoRoot, destDir string) error {
	src, err := os.ReadFile(filepath.Join(repoRoot, "go.sum"))
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(destDir, "go.sum"), src, 0o644)
}

func RepoGoVersion(repoRoot string) (string, error) {
	data, err := os.ReadFile(filepath.Join(repoRoot, "go.mod"))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "go" {
			return fields[1], nil
		}
	}
	return "", fmt.Errorf("go directive not found in %s/go.mod", repoRoot)
}
