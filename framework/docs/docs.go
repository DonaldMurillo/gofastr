// Package docs ships the framework's user-facing markdown docs as an
// embedded filesystem. Two consumers:
//
//  1. `gofastr docs` CLI subcommand — list + show + search.
//  2. `framework.WithMCPIntrospection()` — exposes framework_docs_list
//     and framework_docs_get tools so agents connected to a running app
//     can answer "how do I use hooks" without leaving the session.
//
// The embedded tree is the source of truth for shipped docs; the repo's
// canonical edit location IS framework/docs/content/. There's no
// generation step — every commit changes the binary's docs the next
// build.
package docs

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

//go:embed content/*.md
var contentFS embed.FS

// Topic describes a single doc file in the embedded tree.
type Topic struct {
	// Name is the short identifier — the filename without .md.
	// Use this with Get().
	Name string

	// Title is the first H1 heading from the file's content, or the
	// humanised file name when no heading is present.
	Title string

	// Summary is the first non-heading paragraph, truncated to ~200
	// chars. Empty when the file has no prose body.
	Summary string

	// Bytes is the size of the underlying markdown in bytes.
	Bytes int
}

// List enumerates every embedded doc topic. Result is sorted by Name so
// the order is stable across builds.
func List() ([]Topic, error) {
	entries, err := fs.ReadDir(contentFS, "content")
	if err != nil {
		return nil, fmt.Errorf("docs: list: %w", err)
	}
	out := make([]Topic, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".md")
		body, err := fs.ReadFile(contentFS, "content/"+e.Name())
		if err != nil {
			continue
		}
		t := Topic{
			Name:    name,
			Title:   extractTitle(body, name),
			Summary: extractSummary(body),
			Bytes:   len(body),
		}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Get returns the raw markdown body for the named topic. Returns a
// "not found" error for unknown names — never panics. Pass the topic
// name without the .md suffix.
func Get(name string) ([]byte, error) {
	if name == "" {
		return nil, fmt.Errorf("docs: get: name required")
	}
	if strings.ContainsAny(name, "/\\.") {
		return nil, fmt.Errorf("docs: get: invalid topic name %q", name)
	}
	body, err := fs.ReadFile(contentFS, "content/"+name+".md")
	if err != nil {
		return nil, fmt.Errorf("docs: topic %q not found", name)
	}
	return body, nil
}

// SearchHit is a single grep match: which topic, which line, the line
// text, and a window of surrounding context for the CLI's --grep
// output.
type SearchHit struct {
	Topic   string
	Line    int
	Heading string // the nearest preceding `# `-level heading, if any
	Excerpt string // the matching line itself
}

// minSearchTermLen is the shortest search term Search will accept.
// Below this, the function returns zero hits without scanning the
// corpus — short terms ("a", "of") match noise and would produce
// thousands of hits.
const minSearchTermLen = 3

// defaultSearchHitCap bounds Search's response when the caller doesn't
// supply an explicit limit. Keeps MCP / narrow-context clients safe
// from oversized responses.
const defaultSearchHitCap = 50

// Search returns every line across all topics that contains the (case-
// insensitive) substring `term`, up to defaultSearchHitCap hits. Use
// SearchWithLimit for a caller-supplied cap.
func Search(term string) ([]SearchHit, error) {
	return SearchWithLimit(term, defaultSearchHitCap)
}

// SearchWithLimit is the explicit-cap variant. limit <= 0 falls back to
// defaultSearchHitCap. The first `limit` matching lines are returned;
// the function stops scanning once the cap is reached so the cost is
// O(limit) for common queries.
func SearchWithLimit(term string, limit int) ([]SearchHit, error) {
	if term == "" || len(term) < minSearchTermLen {
		return nil, nil
	}
	if limit <= 0 {
		limit = defaultSearchHitCap
	}
	needle := strings.ToLower(term)
	topics, err := List()
	if err != nil {
		return nil, err
	}
	var hits []SearchHit
	for _, t := range topics {
		body, err := Get(t.Name)
		if err != nil {
			continue
		}
		lines := strings.Split(string(body), "\n")
		var lastHeading string
		for i, ln := range lines {
			if strings.HasPrefix(ln, "#") {
				lastHeading = strings.TrimSpace(strings.TrimLeft(ln, "# "))
				continue
			}
			lower := strings.ToLower(ln)
			if !strings.Contains(lower, needle) {
				continue
			}
			hits = append(hits, SearchHit{
				Topic:   t.Name,
				Line:    i + 1,
				Heading: lastHeading,
				Excerpt: excerptAround(ln, lower, needle, 240),
			})
			if len(hits) >= limit {
				return hits, nil
			}
		}
	}
	return hits, nil
}

// extractTitle returns the first H1 heading or a humanised fallback.
func extractTitle(body []byte, fallback string) string {
	for _, ln := range strings.Split(string(body), "\n") {
		ln = strings.TrimSpace(ln)
		if strings.HasPrefix(ln, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(ln, "# "))
		}
	}
	return humanise(fallback)
}

// extractSummary returns the first non-empty, non-heading paragraph.
// Truncated to ~200 chars so summaries fit on one terminal line.
func extractSummary(body []byte) string {
	for _, ln := range strings.Split(string(body), "\n") {
		t := strings.TrimSpace(ln)
		if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, "<!--") {
			continue
		}
		if len(t) > 200 {
			t = t[:200] + "…"
		}
		return t
	}
	return ""
}

// excerptAround returns a substring of `line` centred on the first
// occurrence of `needle` (matched case-insensitively against `lower`,
// which must be strings.ToLower(line)) and capped at `cap` chars.
// Prepends/appends "…" when the cut hits before/after the match.
func excerptAround(line, lower, needle string, cap int) string {
	if len(line) <= cap {
		return line
	}
	idx := strings.Index(lower, needle)
	half := cap / 2
	start := idx - half
	end := idx + len(needle) + half
	if start < 0 {
		end -= start
		start = 0
	}
	if end > len(line) {
		shift := end - len(line)
		end = len(line)
		start -= shift
		if start < 0 {
			start = 0
		}
	}
	out := line[start:end]
	if start > 0 {
		out = "…" + out
	}
	if end < len(line) {
		out = out + "…"
	}
	return out
}

// humanise turns "entity-declarations" → "Entity declarations".
func humanise(s string) string {
	s = strings.ReplaceAll(s, "-", " ")
	s = strings.ReplaceAll(s, "_", " ")
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
