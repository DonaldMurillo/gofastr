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

// Field weights: a word in the title outranks one in a heading, which
// outranks one in body text. A word's count in one field stops adding
// after tfCap, so a page that repeats a term is not a better match for
// it.
const (
	weightTitle   = 20
	weightHeading = 6
	weightBody    = 1
	tfCap         = 3
)

// titleBonus lifts a page whose title holds every query term above
// every page whose title does not, so a page named for the query is
// its answer however often other pages repeat the words. A title is a
// row of slots, one per camelCase part (a plain word is one slot), and
// the bonus is the share of two points a slot the query earns: a term
// that names a word whole fills all its slots with two, one that names
// a part fills that slot with two, and one that only begins a word
// puts one in its first slot. Each slot keeps its best, so two terms
// never earn one word twice. "table" finds DataTable (2 of 4) before
// TableOfContents (2 of 6), "datatable" finds DataTable (4 of 4)
// before Datatables (1 of 2), and "form format" finds Form Format
// Options (4 of 6) before Formatter (1 of 2). Scores stay far below
// 2^53, so the browser adds them exactly.
const titleBonus = 1_000_000

// excerptRunes caps the section text an index keeps for excerpts. The
// static fragments ship the same capped text, so the browser excerpts
// what the server would.
const excerptRunes = 2000

// Index is a built search index over a set of pages. Every number in
// it is an integer fixed at build time, so a search run in Go and the
// same search run by the browser client over the static files rank
// identically.
type Index struct {
	pages  []Page // section text capped at excerptRunes
	titleN []int  // each page's count of title slots (titleBonus)
	terms  map[string]termEntry
	keys   []string // sorted terms, for prefix scans
}

// termEntry is one term's inverse document frequency (scaled by 100)
// and its postings.
type termEntry struct {
	idf      int64
	postings []posting
}

// posting is one section that holds a term and the term's weighted
// count there. On a page's first section it also says which title
// slots the term names: tLen slots from tFrom, none when tLen is 0.
type posting struct {
	page, section, score int
	tFrom, tLen          int
}

// titleSlots lays a title out as slots (titleBonus): each distinct
// word takes one slot per camelCase part, or one when it has none, and
// names them all whole; each part names its own slot. A term the title
// holds twice keeps its widest span. It returns the spans by term and
// the slot count.
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
		lower := strings.ToLower(word)
		if len(terms) == 0 || seen[lower] {
			continue
		}
		seen[lower] = true
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

// Build indexes pages. A page without a URL is dropped, and so is one
// whose URL, or whose title and text, an earlier page already has: a
// route rendered under two paths, or a demo shown in five themes, is
// one result.
func Build(pages []Page) *Index {
	idx := &Index{terms: map[string]termEntry{}}
	df := map[string]int{}
	seenURL := map[string]bool{}
	seenBody := map[[32]byte]bool{}
	for _, p := range pages {
		if p.URL == "" || seenURL[p.URL] {
			continue
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

// addField adds text's terms to scores at weight w, each word's count
// capped at tfCap.
func addField(scores map[string]int, text string, w int) {
	if text == "" {
		return
	}
	tf := map[string]int{}
	for _, word := range sourceWords(text) {
		for _, t := range indexTerms(word) {
			tf[t]++
		}
	}
	for t, c := range tf {
		scores[t] += w * min(c, tfCap)
	}
}

// bodyHash identifies a page by its title and section text, length
// prefixed so no two different pages join to the same bytes.
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
	return string([]rune(s)[:n])
}

// matching returns the index terms a query term matches: every term q
// prefixes (q itself among them), every term its stem prefixes when it
// has one, and its gerund when the index holds it.
func (idx *Index) matching(q, stem, gerund string) []string {
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
