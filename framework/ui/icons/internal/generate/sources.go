package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
)

type sourceManifest struct {
	SchemaVersion int               `json:"schema_version"`
	Sources       []upstreamSource  `json:"sources"`
	Icons         []sourceIcon      `json:"icons"`
	Aliases       map[string]string `json:"aliases"`
}

type upstreamSource struct {
	ID            string        `json:"id"`
	Name          string        `json:"name"`
	RepositoryURL string        `json:"repository_url"`
	Commit        string        `json:"commit"`
	RetrievedAt   string        `json:"retrieved_at"`
	License       sourceLicense `json:"license"`
}

type sourceLicense struct {
	SPDX         string `json:"spdx"`
	VendoredPath string `json:"vendored_path"`
	SourceURL    string `json:"source_url"`
	SHA256       string `json:"sha256"`
}

type sourceIcon struct {
	Name          string   `json:"name"`
	Source        string   `json:"source"`
	RetrievedAt   string   `json:"retrieved_at"`
	VendoredPath  string   `json:"vendored_path"`
	UpstreamPath  string   `json:"upstream_path"`
	SourceURL     string   `json:"source_url"`
	SHA256        string   `json:"sha256"`
	Modifications []string `json:"modifications"`
}

var (
	commitPattern       = regexp.MustCompile(`^[0-9a-f]{40}$`)
	upstreamNamePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)
	repositoryPattern   = regexp.MustCompile(`^https://github\.com/[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	sourceLabelPattern  = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9 ]*$`)
)

// loadSources verifies the vendored bytes without reaching the network. Paths
// are canonical, and os.Root confines file reads to the icon package directory.
func loadSources(dir string) (sourceManifest, string, error) {
	var m sourceManifest
	root, err := os.OpenRoot(dir)
	if err != nil {
		return m, "", err
	}
	defer root.Close()
	data, err := root.ReadFile("sources.json")
	if err != nil {
		return m, "", err
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return m, "", fmt.Errorf("sources.json: %w", err)
	}
	if m.SchemaVersion != 2 || len(m.Sources) == 0 {
		return m, "", fmt.Errorf("sources.json: expected pinned upstream sources at schema version 2")
	}
	sources := map[string]upstreamSource{}
	var notices strings.Builder
	for _, source := range m.Sources {
		if !namePattern.MatchString(source.ID) || !sourceLabelPattern.MatchString(source.Name) || !repositoryPattern.MatchString(source.RepositoryURL) || !commitPattern.MatchString(source.Commit) {
			return m, "", fmt.Errorf("sources.json: invalid or unpinned upstream source %q", source.ID)
		}
		if _, exists := sources[source.ID]; exists {
			return m, "", fmt.Errorf("sources.json: duplicate upstream source %q", source.ID)
		}
		if _, err := time.Parse(time.RFC3339, source.RetrievedAt); err != nil {
			return m, "", fmt.Errorf("sources.json: source %s retrieved_at must be an RFC3339 timestamp", source.ID)
		}
		if source.License.SPDX != "MIT" && source.License.SPDX != "ISC" && source.License.SPDX != "ISC AND MIT" {
			return m, "", fmt.Errorf("sources.json: unsupported license for %s", source.ID)
		}
		if source.License.VendoredPath != "LICENSE."+source.ID || source.License.SourceURL != sourceBase(source)+"LICENSE" {
			return m, "", fmt.Errorf("sources.json: missing pinned license source for %s", source.ID)
		}
		license, err := root.ReadFile(source.License.VendoredPath)
		if err != nil {
			return m, "", err
		}
		if len(license) == 0 || !matchesSHA256(license, source.License.SHA256) {
			return m, "", fmt.Errorf("%s differs from its recorded SHA-256", source.License.VendoredPath)
		}
		notices.WriteString(source.Name + " source license:\n")
		notices.Write(license)
		notices.WriteString("\n")
		sources[source.ID] = source
	}
	if len(m.Icons) == 0 {
		return m, "", fmt.Errorf("sources.json: no icons recorded")
	}
	seen := map[string]bool{}
	for _, icon := range m.Icons {
		source, ok := sources[icon.Source]
		if !ok {
			return m, "", fmt.Errorf("sources.json: unknown source %q for %s", icon.Source, icon.Name)
		}
		if _, err := time.Parse(time.RFC3339, icon.RetrievedAt); err != nil {
			return m, "", fmt.Errorf("sources.json: %s retrieved_at must be an RFC3339 timestamp", icon.Name)
		}
		upstreamName := strings.TrimSuffix(strings.TrimPrefix(icon.UpstreamPath, "icons/"), ".svg")
		if !namePattern.MatchString(icon.Name) || seen[icon.Name] || icon.VendoredPath != "svg/"+icon.Name+".svg" || !upstreamNamePattern.MatchString(upstreamName) || icon.UpstreamPath != "icons/"+upstreamName+".svg" || icon.SourceURL != sourceBase(source)+icon.UpstreamPath {
			return m, "", fmt.Errorf("sources.json: invalid, duplicate, or unpinned icon source %q", icon.Name)
		}
		if icon.Modifications == nil || len(icon.Modifications) != 0 {
			return m, "", fmt.Errorf("sources.json: %s must record an empty modifications array for its unmodified upstream SVG", icon.Name)
		}
		seen[icon.Name] = true
		data, err := root.ReadFile(icon.VendoredPath)
		if err != nil {
			return m, "", err
		}
		if !matchesSHA256(data, icon.SHA256) {
			return m, "", fmt.Errorf("%s differs from its recorded upstream SHA-256", icon.VendoredPath)
		}
	}
	for alias, target := range m.Aliases {
		if !namePattern.MatchString(alias) || seen[alias] || !seen[target] {
			return m, "", fmt.Errorf("sources.json: alias %q must name one canonical icon without shadowing a drawing", alias)
		}
	}
	return m, notices.String(), nil
}

func sourceBase(source upstreamSource) string {
	return "https://raw.githubusercontent.com/" + strings.TrimPrefix(source.RepositoryURL, "https://github.com/") + "/" + source.Commit + "/"
}

func matchesSHA256(data []byte, want string) bool {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]) == want
}
