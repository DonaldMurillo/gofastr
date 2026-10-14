package sitesearch

import (
	"cmp"
	"slices"
	"strings"
)

// Result is one page that matched a query.
type Result struct {
	URL   string `json:"url"`
	Title string `json:"title"`
	Group string `json:"group,omitempty"`
	// Excerpt is the page's opening around its first hit. It is empty
	// when the hits are all in Sections, whose rows show them.
	Excerpt []Segment `json:"excerpt,omitempty"`
	// Sections are the page's best-matching headed sections, most
	// relevant first, at most maxSectionHits.
	Sections []SectionHit `json:"sections,omitempty"`
	Score    int64        `json:"score"`
}

// SectionHit is one headed section of a result page that matched.
type SectionHit struct {
	ID      string    `json:"id,omitempty"`
	Heading string    `json:"heading"`
	Excerpt []Segment `json:"excerpt"`
}

// Segment is one run of excerpt text; Hit marks a run that matched a
// query term. Clients render runs as text nodes, never as markup.
type Segment struct {
	Text string `json:"t"`
	Hit  bool   `json:"h,omitempty"`
}

// maxSectionHits caps the section rows under one result.
const maxSectionHits = 3

// Search runs q against the index and returns at most limit pages,
// best first. Every query term must match a page (the terms are
// ANDed); a term matches an index term it equals or prefixes, an exact
// match weighing twice a prefix one, and a plural term ("forms") also
// matches what its singular prefixes. Ties break by index order.
func (idx *Index) Search(q string, limit int) []Result {
	terms := queryTerms(q)
	if len(terms) == 0 || limit <= 0 {
		return nil
	}
	type key struct{ page, section int }
	var pageScore map[int]int64
	secScore := map[key]int64{}
	for qi, qt := range terms {
		perPage := map[int]int64{}
		perSec := map[key]int64{}
		for _, t := range idx.matching(qt, stem(qt)) {
			e := idx.terms[t]
			mult := int64(1)
			if t == qt {
				mult = 2
			}
			// A page scores its best section in full and a quarter of
			// the rest, so a long page that mentions a term in every
			// section does not outrank the page about it.
			sums, best := map[int]int64{}, map[int]int64{}
			for _, p := range e.postings {
				v := e.idf * mult * int64(p.score)
				sums[p.page] += v
				best[p.page] = max(best[p.page], v)
				k := key{p.page, p.section}
				perSec[k] = max(perSec[k], v)
			}
			for pg, sum := range sums {
				perPage[pg] = max(perPage[pg], best[pg]+(sum-best[pg])/4)
			}
		}
		if qi == 0 {
			pageScore = perPage
		} else {
			for pg := range pageScore {
				if v, ok := perPage[pg]; ok {
					pageScore[pg] += v
				} else {
					delete(pageScore, pg)
				}
			}
		}
		for k, v := range perSec {
			secScore[k] += v
		}
		if len(pageScore) == 0 {
			return nil
		}
	}
	order := make([]int, 0, len(pageScore))
	for pg := range pageScore {
		order = append(order, pg)
	}
	slices.SortFunc(order, func(a, b int) int {
		if c := cmp.Compare(pageScore[b], pageScore[a]); c != 0 {
			return c
		}
		return cmp.Compare(a, b)
	})
	if len(order) > limit {
		order = order[:limit]
	}
	out := make([]Result, 0, len(order))
	for _, pg := range order {
		p := idx.pages[pg]
		r := Result{URL: p.URL, Title: p.Title, Group: p.Group, Score: pageScore[pg]}
		// A section is a row only when it can be linked to: a heading
		// without an id still counts toward its page.
		var hits []int
		for si, s := range p.Sections {
			if si > 0 && s.ID != "" && secScore[key{pg, si}] > 0 {
				hits = append(hits, si)
			}
		}
		slices.SortStableFunc(hits, func(a, b int) int {
			return cmp.Compare(secScore[key{pg, b}], secScore[key{pg, a}])
		})
		if len(hits) > maxSectionHits {
			hits = hits[:maxSectionHits]
		}
		for _, si := range hits {
			s := p.Sections[si]
			r.Sections = append(r.Sections, SectionHit{ID: s.ID, Heading: s.Heading, Excerpt: excerpt(s.Text, terms)})
		}
		// The page's own row shows its opening; when only headed
		// sections matched, their rows carry the hits and the page row
		// shows none rather than repeat the first.
		if secScore[key{pg, 0}] > 0 || len(hits) == 0 {
			r.Excerpt = excerpt(p.Sections[0].Text, terms)
		}
		out = append(out, r)
	}
	return out
}

// stem is the singular a plural query term also matches as a prefix:
// the term less a final s, for terms of four or more runes not ending
// in ss. Empty when there is none.
func stem(q string) string {
	rs := []rune(q)
	if len(rs) < 4 || rs[len(rs)-1] != 's' || rs[len(rs)-2] == 's' {
		return ""
	}
	return string(rs[:len(rs)-1])
}

// Excerpt window, in runes: the window opens up to excerptLead runes
// before the first hit and spans excerptWidth.
const (
	excerptLead  = 40
	excerptWidth = 160
)

// excerpt cuts the window of text around the first word a query term
// prefixes and splits it into plain and hit runs. The window's edges
// snap to spaces so no word is cut, and an ellipsis marks a cut end.
func excerpt(text string, terms []string) []Segment {
	rs := []rune(text)
	var hits []token
	for _, t := range scan(text) {
		for _, q := range terms {
			if strings.HasPrefix(t.word, q) {
				hits = append(hits, t)
				break
			}
		}
	}
	start := 0
	if len(hits) > 0 && hits[0].start > excerptLead {
		start = hits[0].start - excerptLead
		if i := indexRune(rs[start:hits[0].start], ' '); i >= 0 {
			start += i + 1
		}
	}
	end := min(len(rs), start+excerptWidth)
	if end < len(rs) {
		from := start
		if len(hits) > 0 {
			from = min(hits[0].end, end)
		}
		if i := lastIndexRune(rs[from:end], ' '); i >= 0 {
			end = from + i
		}
	}
	var segs []Segment
	plain := func(s string) {
		if s == "" {
			return
		}
		if n := len(segs); n > 0 && !segs[n-1].Hit {
			segs[n-1].Text += s
			return
		}
		segs = append(segs, Segment{Text: s})
	}
	if start > 0 {
		plain("…")
	}
	at := start
	for _, h := range hits {
		if h.start < start || h.end > end {
			continue
		}
		plain(string(rs[at:h.start]))
		segs = append(segs, Segment{Text: string(rs[h.start:h.end]), Hit: true})
		at = h.end
	}
	plain(string(rs[at:end]))
	if end < len(rs) {
		plain("…")
	}
	return segs
}

func indexRune(rs []rune, r rune) int {
	for i, c := range rs {
		if c == r {
			return i
		}
	}
	return -1
}

func lastIndexRune(rs []rune, r rune) int {
	for i := len(rs) - 1; i >= 0; i-- {
		if rs[i] == r {
			return i
		}
	}
	return -1
}
