package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var exampleAbsoluteURLRe = regexp.MustCompile(`(?i)https://([A-Za-z0-9._-]+)`)

// allowedExampleHost reports whether an example app may name host in
// code: a reserved documentation domain, loopback, a spec or schema
// host, or a host the project serves from.
func allowedExampleHost(host string) bool {
	switch host {
	case "example.com", "example.net", "example.org",
		"localhost", "127.0.0.1", "::1",
		"schema.org", "www.schema.org", "www.sitemaps.org", "www.w3.org",
		"llmstxt.org", "spec.modelcontextprotocol.io", "modelcontextprotocol.io",
		"pkg.go.dev", "go.dev", "github.com", "donaldmurillo.github.io",
		"barcode.donaldmurillo.com", "img.shields.io", "keepachangelog.com",
		"semver.org", "creativecommons.org", "opensource.org", "www.rfc-editor.org":
		return true
	}
	return strings.HasSuffix(host, ".example") ||
		strings.HasSuffix(host, ".example.com") ||
		strings.HasSuffix(host, ".example.net") ||
		strings.HasSuffix(host, ".example.org")
}

func lintExampleOrigins(root string) ([]finding, error) {
	var findings []finding
	for _, dir := range []string{"examples", filepath.Join("internal", "testapps")} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				if os.IsNotExist(err) {
					return nil
				}
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				rel = path
			}
			seen := map[string]bool{}
			var lex goLexState
			for i, raw := range strings.Split(string(body), "\n") {
				var line string
				line, lex = stripGoComments(raw, lex)
				for _, m := range exampleAbsoluteURLRe.FindAllStringSubmatch(line, -1) {
					host := m[1]
					if allowedExampleHost(host) || seen[host] {
						continue
					}
					seen[host] = true
					findings = append(findings, finding{
						File:    filepath.ToSlash(rel),
						Line:    i + 1,
						Rule:    "example-dead-origin",
						Message: "example advertises origin " + host + " : use a reserved documentation domain (example.com) for a demo that is not deployed, or a host the project actually serves",
					})
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return findings, nil
}
