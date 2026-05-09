package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/gofastr/gofastr/core/markdown"
)

// docCatalog is a process-scoped index of available docs. It scans the docs
// directory once on first use, parses each Markdown file's frontmatter, and
// returns ordered metadata so /docs/ and /docs/:slug stay in sync.
//
// Generation runs read at build time; SSR runs read on first request. We
// intentionally cache after the first scan — for a docs site, content is
// stable enough that re-reading on every render is wasteful.
type docCatalog struct {
	once  sync.Once
	root  string
	items []docItem
}

type docItem struct {
	Slug        string // filename minus .md, e.g. "migrations"
	Title       string // first H1 or frontmatter title; falls back to a humanised slug
	Description string // first paragraph (best effort)
	Path        string // absolute path to the Markdown file
}

var docs = &docCatalog{}

// load is idempotent: subsequent calls reuse the first scan.
func (c *docCatalog) load() error {
	var loadErr error
	c.once.Do(func() {
		root, err := findDocsRoot()
		if err != nil {
			loadErr = err
			return
		}
		c.root = root

		entries, err := os.ReadDir(root)
		if err != nil {
			loadErr = err
			return
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			path := filepath.Join(root, e.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			doc := markdown.Render(string(data))
			slug := strings.TrimSuffix(e.Name(), ".md")
			title := doc.Title
			if title == "" {
				title = humanise(slug)
			}
			c.items = append(c.items, docItem{
				Slug:        slug,
				Title:       title,
				Description: firstParagraphPreview(string(data), 160),
				Path:        path,
			})
		}
		sort.Slice(c.items, func(i, j int) bool { return c.items[i].Title < c.items[j].Title })
	})
	return loadErr
}

func (c *docCatalog) all() ([]docItem, error) {
	if err := c.load(); err != nil {
		return nil, err
	}
	return c.items, nil
}

func (c *docCatalog) find(slug string) (docItem, error) {
	if err := c.load(); err != nil {
		return docItem{}, err
	}
	for _, item := range c.items {
		if item.Slug == slug {
			return item, nil
		}
	}
	return docItem{}, fmt.Errorf("docs: no doc with slug %q", slug)
}

// findDocsRoot locates the repo's top-level docs/ directory regardless of
// where the binary was invoked from. We try the cwd first (for `go run` and
// the dev server) then walk up looking for a sibling docs/ directory.
func findDocsRoot() (string, error) {
	candidates := []string{
		"docs",
		filepath.Join("..", "..", "docs"),
		filepath.Join("..", "..", "..", "docs"),
	}
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && info.IsDir() {
			abs, _ := filepath.Abs(c)
			return abs, nil
		}
	}
	return "", fmt.Errorf("docs: could not locate docs/ directory; tried %v", candidates)
}

func humanise(slug string) string {
	parts := strings.Split(slug, "-")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}

// firstParagraphPreview pulls the first non-empty, non-heading line out of
// the source, strips Markdown punctuation, and truncates to maxLen. Used
// for /docs/ index card descriptions.
func firstParagraphPreview(src string, maxLen int) string {
	lines := strings.Split(src, "\n")
	skipFM := false
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if i == 0 && t == "---" {
			skipFM = true
			continue
		}
		if skipFM {
			if t == "---" {
				skipFM = false
			}
			continue
		}
		if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, ">") || strings.HasPrefix(t, "```") {
			continue
		}
		t = stripInline(t)
		if len(t) > maxLen {
			t = t[:maxLen-1] + "…"
		}
		return t
	}
	return ""
}

// stripInline removes the loud Markdown markers from a line of text so it
// can be displayed as plain prose in an index card. Not exhaustive — just
// the common ones.
func stripInline(s string) string {
	r := strings.NewReplacer("**", "", "*", "", "`", "", "__", "", "_", "")
	return r.Replace(s)
}
