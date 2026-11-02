// Package docs ships the framework's user-facing markdown docs as an
// embedded filesystem. Two consumers:
//
//  1. `gofastr docs` CLI subcommand: list + show + search.
//  2. `framework.WithMCPTools(mcptools.Register)` (framework/docs/mcptools): exposes the
//     framework_docs_list / framework_docs_get / framework_docs_search MCP
//     tools so agents connected to a running app can answer "how do I
//     use hooks" without leaving the session.
//
// Package framework does not import this package: the corpus is about
// 2 MB of markdown and a docs-only edit must not widen the
// affected test set to every framework importer (#470). Anything that
// needs the corpus at runtime imports it from the leaf, never from the
// App spine.
//
// The embedded tree is the source of truth for shipped docs; the repo's
// canonical edit location IS framework/docs/content/. There's no
// generation step: every commit changes the binary's docs the next
// build.
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
	// Name is the short identifier: the filename without .md.
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
// "not found" error for unknown names. Never panics. Pass the topic
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
// Below this, the function returns an error without scanning the
// corpus: short terms ("a", "of") match noise and would produce
// thousands of hits.
const minSearchTermLen = 3

// defaultSearchHitCap bounds Search's response when the caller doesn't
// supply an explicit limit. Keeps MCP / narrow-context clients safe
// from oversized responses.
const defaultSearchHitCap = 50

// ErrSearchTermTooShort is the error Search and SearchWithLimit return
// for a term under three bytes, the empty term included, so a caller
// can tell a term too short to search from one that matched nothing.
var ErrSearchTermTooShort = fmt.Errorf("docs: a search term needs at least %d characters", minSearchTermLen)

// Search returns the matching lines across all topics, up to
// defaultSearchHitCap hits, ordered by TOPIC RELEVANCE: a topic whose
// title matches the term first, then topics with more heading matches,
// then topics with more body matches, alphabetical only as the
// tie-break. Within a topic the hits keep their line order. Use
// SearchWithLimit for a caller-supplied cap.
func Search(term string) ([]SearchHit, error) {
	return SearchWithLimit(term, defaultSearchHitCap)
}

// topicScore is one topic's relevance for a term, tiered by WHERE the
// term matches: a title match beats everything (the term names the
// topic's subject), a match in the topic's own lede (the summary
// before the first section heading — what the topic says it is about)
// beats section headings, a heading match means the topic owns a
// section on the term, and a body line is a mention. HOW OFTEN counts
// within each tier; the topic name is the final tie-break.
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

// SearchWithLimit is the explicit-cap variant. limit <= 0 falls back to
// defaultSearchHitCap. Every topic is scanned (relevance needs the
// whole corpus's counts); the ranked result is capped at `limit` hits.
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
	// Ranked order: relevance first, alphabetical as the tie-break
	// (sort.SliceStable over an already name-sorted slice makes the
	// comparison deterministic).
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

// scanTopic scores one topic for needle (already lowercased) and
// returns its matching lines in order. A heading line is never a hit;
// a `#` line inside a fenced code block is code (a shell or YAML
// comment), so it is an ordinary line, not a heading.
func scanTopic(name string, body []byte, needle string) (topicScore, []SearchHit) {
	score := topicScore{name: name, title: strings.Contains(strings.ToLower(extractTitle(body, name)), needle)}
	lines := strings.Split(string(body), "\n")
	var lastHeading string
	lede := true // lines before the first section heading
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

// fenceMarker reports whether a line opens or closes a fenced code block.
func fenceMarker(line string) bool {
	t := strings.TrimSpace(line)
	return strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~")
}

// extractTitle returns the first H1 heading outside a fenced block, or
// a humanised fallback.
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

// extractSummary returns the first non-empty, non-heading paragraph.
// Truncated to ~200 chars so summaries fit on one terminal line.
func extractSummary(body []byte) string {
	for ln := range strings.SplitSeq(string(body), "\n") {
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

// matchSpan returns the byte span of line whose lowering is the first
// match of needle in lower (strings.ToLower(line)). The two strings'
// offsets differ wherever lowering changes a rune's length (U+0130 'İ'
// is 2 bytes and lowers to 1; an invalid byte becomes the 3-byte
// U+FFFD), so the span is found by lowering line rune by rune, the way
// strings.ToLower does, until the lowered length reaches the match.
func matchSpan(line, lower, needle string) (start, end int) {
	at := strings.Index(lower, needle)
	if at < 0 {
		return 0, 0
	}
	start, end = -1, len(line)
	off := 0 // length of line[:i] once lowered
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

// excerptAround returns a substring of `line` centred on the first
// occurrence of `needle` (matched case-insensitively against `lower`,
// which must be strings.ToLower(line)): cap/2 chars either side of the
// match, shifted inward at the line's ends, so a cut line is cap chars
// plus the needle. Prepends/appends "…" when the cut hits before/after
// the match. The cut widens to whole runes, so the excerpt is valid
// UTF-8 whenever line is.
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

// humanise turns "entity-declarations" → "Entity declarations".
func humanise(s string) string {
	s = strings.ReplaceAll(s, "-", " ")
	s = strings.ReplaceAll(s, "_", " ")
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
