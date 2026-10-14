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

// excerptRunes caps the section text an index keeps for excerpts. The
// static fragments ship the same capped text, so the browser excerpts
// what the server would.
const excerptRunes = 2000

// Index is a built search index over a set of pages. Every number in
// it is an integer fixed at build time, so a search run in Go and the
// same search run by the browser client over the static files rank
// identically.
type Index struct {
	pages []Page // section text capped at excerptRunes
	terms map[string]termEntry
	keys  []string // sorted terms, for prefix scans
}

// termEntry is one term's inverse document frequency (scaled by 100)
// and its postings.
type termEntry struct {
	idf      int64
	postings []posting
}

// posting is one section that holds a term, and the term's weighted
// count there.
type posting struct {
	page, section, score int
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
		for si, s := range p.Sections {
			scores := map[string]int{}
			if si == 0 {
				addField(scores, p.Title, weightTitle)
			}
			addField(scores, s.Heading, weightHeading)
			addField(scores, s.Text, weightBody)
			for _, t := range slices.Sorted(maps.Keys(scores)) {
				e := idx.terms[t]
				e.postings = append(e.postings, posting{page: pi, section: si, score: scores[t]})
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
// prefixes (q itself among them), and every term its stem prefixes
// when it has one.
func (idx *Index) matching(q, stem string) []string {
	from := q
	if stem != "" {
		from = stem
	}
	i, _ := slices.BinarySearch(idx.keys, from)
	var out []string
	for ; i < len(idx.keys) && strings.HasPrefix(idx.keys[i], from); i++ {
		out = append(out, idx.keys[i])
	}
	return out
}
