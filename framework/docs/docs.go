// Package docs ships the framework's user docs (framework/docs/content)
// as an embedded filesystem, read by List, Get and Search for the
// `gofastr docs` command and the framework_docs_* MCP tools in
// framework/docs/mcptools.
package docs

import (
	"embed"
	"fmt"
	"io/fs"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

//go:embed content/*.md
var contentFS embed.FS

// Topic describes a single doc file in the embedded tree.
type Topic struct {
	// Name is the filename without .md, the name Get takes.
	Name string

	// Title is the first H1 heading, or the humanised file name.
	Title string

	// Summary is the first paragraph of prose, cut to about 200 bytes.
	Summary string

	// Bytes is the markdown's size.
	Bytes int
}

// List returns every embedded doc topic, sorted by Name.
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

// Get returns the markdown of the named topic (no .md suffix), or an
// error for an unknown or malformed name.
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

// SearchHit is one matching line.
type SearchHit struct {
	Topic string
	Line  int
	// Heading is the nearest heading above the line, if any.
	Heading string
	// Excerpt is the matching line, cut around the match.
	Excerpt string
}

const minSearchTermLen = 3

const defaultSearchHitCap = 50

// ErrSearchTermTooShort is returned for a term under three bytes.
var ErrSearchTermTooShort = fmt.Errorf("docs: a search term needs at least %d characters", minSearchTermLen)

// Search returns up to 50 case-insensitive matching lines across all
// topics, the most relevant topics first and each topic's lines in order.
func Search(term string) ([]SearchHit, error) {
	return SearchWithLimit(term, defaultSearchHitCap)
}

type topicScore struct {
	name     string
	title    bool
	lede     int
	headings int
	body     int
}

func (a topicScore) moreRelevant(b topicScore) bool {
	if a.title != b.title {
		return a.title
	}
	if a.lede != b.lede {
		return a.lede > b.lede
	}
	if a.headings != b.headings {
		return a.headings > b.headings
	}
	if a.body != b.body {
		return a.body > b.body
	}
	return a.name < b.name
}

// SearchWithLimit is Search capped at limit hits; limit <= 0 means 50.
func SearchWithLimit(term string, limit int) ([]SearchHit, error) {
	if len(term) < minSearchTermLen {
		return nil, ErrSearchTermTooShort
	}
	if limit <= 0 {
		limit = defaultSearchHitCap
	}
	needle := strings.ToLower(term)
	topics, err := List()
	if err != nil {
		return nil, err
	}
	perTopic := make(map[string][]SearchHit)
	scores := make([]topicScore, 0, len(topics))
	for _, t := range topics {
		body, err := Get(t.Name)
		if err != nil {
			continue
		}
		score, hits := scanTopic(t.Name, body, needle)
		if len(hits) > 0 {
			perTopic[t.Name] = hits
			scores = append(scores, score)
		}
	}
	slices.SortStableFunc(scores, func(a, b topicScore) int {
		if a.moreRelevant(b) {
			return -1
		}
		if b.moreRelevant(a) {
			return 1
		}
		return 0
	})
	var out []SearchHit
	for _, s := range scores {
		for _, h := range perTopic[s.name] {
			out = append(out, h)
			if len(out) >= limit {
				return out, nil
			}
		}
	}
	return out, nil
}

func scanTopic(name string, body []byte, needle string) (topicScore, []SearchHit) {
	score := topicScore{name: name, title: strings.Contains(strings.ToLower(extractTitle(body, name)), needle)}
	lines := strings.Split(string(body), "\n")
	var lastHeading string
	lede := true
	var hits []SearchHit
	inFence := false
	for i, ln := range lines {
		if fenceMarker(ln) {
			inFence = !inFence
		}
		if !inFence && strings.HasPrefix(ln, "#") {
			if strings.HasPrefix(ln, "##") {
				lede = false
			}
			lastHeading = strings.TrimSpace(strings.TrimLeft(ln, "# "))
			if strings.Contains(strings.ToLower(lastHeading), needle) {
				score.headings++
			}
			continue
		}
		lower := strings.ToLower(ln)
		if !strings.Contains(lower, needle) {
			continue
		}
		if lede {
			score.lede++
		}
		score.body++
		hits = append(hits, SearchHit{
			Topic:   name,
			Line:    i + 1,
			Heading: lastHeading,
			Excerpt: excerptAround(ln, lower, needle, 240),
		})
	}
	return score, hits
}

func fenceMarker(line string) bool {
	t := strings.TrimSpace(line)
	return strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~")
}

func extractTitle(body []byte, fallback string) string {
	inFence := false
	for ln := range strings.SplitSeq(string(body), "\n") {
		if fenceMarker(ln) {
			inFence = !inFence
		}
		ln = strings.TrimSpace(ln)
		if after, ok := strings.CutPrefix(ln, "# "); ok && !inFence {
			return strings.TrimSpace(after)
		}
	}
	return humanise(fallback)
}

func extractSummary(body []byte) string {
	var para []string
	inFence, inComment := false, false
	for ln := range strings.SplitSeq(string(body), "\n") {
		t := strings.TrimSpace(ln)
		if inComment {
			inComment = !strings.Contains(t, "-->")
			continue
		}
		if fenceMarker(ln) {
			if len(para) > 0 {
				break
			}
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, "<!--") {
			if len(para) > 0 {
				break
			}
			inComment = strings.HasPrefix(t, "<!--") && !strings.Contains(t, "-->")
			continue
		}
		t = strings.TrimSpace(strings.TrimPrefix(t, ">"))
		if t != "" {
			para = append(para, t)
		}
	}
	out := strings.Join(para, " ")
	if len(out) > 200 {
		cut := 200
		for cut > 0 && !utf8.RuneStart(out[cut]) {
			cut--
		}
		out = out[:cut] + "…"
	}
	return out
}

func matchSpan(line, lower, needle string) (start, end int) {
	at := strings.Index(lower, needle)
	if at < 0 {
		return 0, 0
	}
	start, end = -1, len(line)
	off := 0
	for i := 0; i < len(line); {
		if start < 0 && off >= at {
			start = i
		}
		if start >= 0 && off >= at+len(needle) {
			end = i
			break
		}
		r, size := utf8.DecodeRuneInString(line[i:])
		off += utf8.RuneLen(unicode.ToLower(r))
		i += size
	}
	if start < 0 {
		start = len(line)
	}
	return start, end
}

func excerptAround(line, lower, needle string, cap int) string {
	if len(line) <= cap {
		return line
	}
	matchStart, matchEnd := matchSpan(line, lower, needle)
	half := cap / 2
	start := matchStart - half
	end := matchEnd + half
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
	for start > 0 && !utf8.RuneStart(line[start]) {
		start--
	}
	for end < len(line) && !utf8.RuneStart(line[end]) {
		end++
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

func humanise(s string) string {
	s = strings.ReplaceAll(s, "-", " ")
	s = strings.ReplaceAll(s, "_", " ")
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
