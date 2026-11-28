package sitesearch

import (
	"crypto/sha256"
	"encoding/binary"
	"maps"
	"math"
	"slices"
	"strings"
	"unicode/utf8"
)

const (
	weightTitle   = 20
	weightHeading = 6
	weightBody    = 1
	tfCap         = 3
)

const titleBonus = 1_000_000

const excerptRunes = 2000

// Index is a built search index over a set of pages.
type Index struct {
	pages  []Page
	titleN []int
	terms  map[string]termEntry
	keys   []string
}

type termEntry struct {
	idf      int64
	postings []posting
}

type posting struct {
	page, section, score int
	tFrom, tLen          int
}

func titleSlots(title string) (map[string][2]int, int) {
	spans := map[string][2]int{}
	keep := func(t string, from, n int) {
		if old, ok := spans[t]; !ok || n > old[1] {
			spans[t] = [2]int{from, n}
		}
	}
	n := 0
	seen := map[string]bool{}
	for _, word := range sourceWords(title) {
		terms := indexTerms(word)
		key := strings.Join(terms, "\x00")
		if len(terms) == 0 || seen[key] {
			continue
		}
		seen[key] = true
		lower := strings.ToLower(word)
		whole := terms[0] == lower
		parts := terms
		if whole {
			parts = terms[1:]
		}
		width := max(1, len(parts))
		if whole {
			keep(lower, n, width)
		}
		for i, part := range parts {
			keep(part, n+i, 1)
		}
		n += width
	}
	return spans, n
}

func sitePath(u string) bool {
	return strings.HasPrefix(u, "/") && !strings.HasPrefix(u, "//") &&
		!strings.ContainsFunc(u, func(r rune) bool { return r == '\\' || r < ' ' || r == 0x7f })
}

// Build indexes pages.
func Build(pages []Page) *Index {
	idx := &Index{terms: map[string]termEntry{}}
	df := map[string]int{}
	seenURL := map[string]bool{}
	seenBody := map[[32]byte]bool{}
	for _, p := range pages {
		if !sitePath(p.URL) || seenURL[p.URL] {
			continue
		}
		// The title is indexed with the lead section, and search reads
		// Sections[0]; a page built with none gets an empty lead, as
		// extraction always gives one.
		if len(p.Sections) == 0 {
			p.Sections = []Section{{}}
		}
		body := bodyHash(p)
		if seenBody[body] {
			continue
		}
		seenURL[p.URL], seenBody[body] = true, true
		pi := len(idx.pages)
		seen := map[string]bool{}
		spans, slots := titleSlots(p.Title)
		for si, s := range p.Sections {
			scores := map[string]int{}
			if si == 0 {
				addField(scores, p.Title, weightTitle)
			}
			addField(scores, s.Heading, weightHeading)
			addField(scores, s.Text, weightBody)
			for _, t := range slices.Sorted(maps.Keys(scores)) {
				e := idx.terms[t]
				post := posting{page: pi, section: si, score: scores[t]}
				if sp, ok := spans[t]; ok && si == 0 {
					post.tFrom, post.tLen = sp[0], sp[1]
				}
				e.postings = append(e.postings, post)
				idx.terms[t] = e
				if !seen[t] {
					seen[t] = true
					df[t]++
				}
			}
		}
		kept := p
		kept.Sections = make([]Section, len(p.Sections))
		for i, s := range p.Sections {
			s.Text = capRunes(s.Text, excerptRunes)
			kept.Sections[i] = s
		}
		idx.pages = append(idx.pages, kept)
		idx.titleN = append(idx.titleN, slots)
	}
	n := float64(len(idx.pages))
	for t, e := range idx.terms {
		e.idf = int64(math.Round(100 * math.Log(1+n/float64(df[t]))))
		idx.terms[t] = e
	}
	idx.keys = slices.Sorted(maps.Keys(idx.terms))
	return idx
}

// Len reports how many pages the index holds.
func (idx *Index) Len() int { return len(idx.pages) }

func addField(scores map[string]int, text string, w int) {
	if text == "" {
		return
	}
	tf := map[string]int{}
	seen := map[string]bool{}
	for _, word := range sourceWords(text) {
		clear(seen)
		for _, t := range indexTerms(word) {
			if !seen[t] {
				tf[t]++
				seen[t] = true
			}
		}
	}
	for t, c := range tf {
		scores[t] += w * min(c, tfCap)
	}
}

func bodyHash(p Page) [32]byte {
	h := sha256.New()
	write := func(s string) {
		var n [8]byte
		binary.BigEndian.PutUint64(n[:], uint64(len(s)))
		h.Write(n[:])
		h.Write([]byte(s))
	}
	write(p.Title)
	for _, s := range p.Sections {
		write(s.ID)
		write(s.Heading)
		write(s.Text)
	}
	var out [32]byte
	h.Sum(out[:0])
	return out
}

func capRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	rs := []rune(s)[:n-1]
	if i := lastIndexRune(rs, ' '); i > 0 {
		rs = rs[:i]
	}
	return string(rs) + "…"
}

func (idx *Index) matching(q, stem, gerund string) []string {
	if utf8.RuneCountInString(q) < minPrefixRunes {
		if _, ok := idx.terms[q]; ok {
			return []string{q}
		}
		return nil
	}
	from := q
	if stem != "" {
		from = stem
	}
	i, _ := slices.BinarySearch(idx.keys, from)
	var out []string
	for ; i < len(idx.keys) && strings.HasPrefix(idx.keys[i], from); i++ {
		out = append(out, idx.keys[i])
	}
	if gerund != "" && !strings.HasPrefix(gerund, from) {
		if _, ok := idx.terms[gerund]; ok {
			out = append(out, gerund)
		}
	}
	return out
}
