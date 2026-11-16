package main

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// vettoolBuild matches a line that builds cmd/vettool: a shell `go build`
// or a Go exec.Command(..., "build", ...) whose target ends at cmd/vettool.
var vettoolBuild = regexp.MustCompile(`(\bgo build\b|"build").*cmd/vettool("|\s|$)`)

// lintVettoolBuildVCS checks that every build of cmd/vettool passes
// -buildvcs=false. go vet keys each cached result on the vettool binary's
// hash, and a plain build in a git checkout stamps the commit into the
// binary, so every new commit changed the hash and re-vetted every package
// and its dependencies from scratch: about 75 s per GOOS, 3.7 minutes of
// every CI run (2026-11), and the same on every local commit through the
// pre-commit hook.
func lintVettoolBuildVCS(root string) ([]finding, error) {
	var findings []finding
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "testdata", "vendor", "dist":
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if !vettoolBuildCandidate(rel) {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for n := 1; sc.Scan(); n++ {
			line := strings.TrimSpace(sc.Text())
			if strings.HasPrefix(line, "//") || strings.HasPrefix(line, "#") {
				continue
			}
			if vettoolBuild.MatchString(line) && !strings.Contains(line, "-buildvcs=false") {
				findings = append(findings, finding{
					File: rel, Line: n, Rule: "vettool-buildvcs",
					Message: "build cmd/vettool with -buildvcs=false: the stamped commit changes the binary's hash on every commit, and go vet then caches nothing",
				})
			}
		}
		return sc.Err()
	})
	return findings, err
}

func vettoolBuildCandidate(rel string) bool {
	if strings.HasPrefix(rel, ".githooks/") || filepath.Base(rel) == "Makefile" {
		return true
	}
	switch filepath.Ext(rel) {
	case ".go", ".sh", ".yml", ".yaml":
		return true
	}
	return false
}
