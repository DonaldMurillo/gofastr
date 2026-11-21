package sitesearch

import (
	"cmp"
	"slices"
	"strings"
	"unicode/utf8"
)

// Result is one page that matched a query.
type Result struct {
	URL   string `json:"url"`
	Title string `json:"title"`
	Group string `json:"group,omitempty"`
	// Excerpt is the page's opening around its first hit.
	Excerpt []Segment `json:"excerpt,omitempty"`
	// Sections are the page's best-matching headed sections, most relevant
	// first, at most maxSectionHits.
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
// query term.
type Segment struct {
	Text string `json:"t"`
	Hit  bool   `json:"h,omitempty"`
}

const maxSectionHits = 3

// Search runs q against the index and returns at most limit pages, best
// first.
func (idx *Index) Search(q string, limit int) []Result {
	out, _ := idx.SearchPage(q, 0, limit)
	return out
}

// SearchPage is Search from the offset-th result on: at most limit
// results past the first offset, and how many pages matched in all, so a
// results page can count them and page through.
func (idx *Index) SearchPage(q string, offset, limit int) ([]Result, int) {
	terms := queryTerms(q)
	if len(terms) == 0 || limit <= 0 {
		return nil, 0
	}
	offset = max(offset, 0)
	n := len(idx.pages)
	pageScore := make([]int64, n)
	matched := make([]int, n)
	inTitle := make([]int, n)
	perPage := make([]int64, n)
	sums, best := make([]int64, n), make([]int64, n)
	titled := make([]bool, n)
	slots := map[int]map[int]int64{}
	matches := make([][]string, len(terms))
	var qPages, tPages []int
	for qi, qt := range terms {
		st, ger := stem(qt), gerund(qt)
		matches[qi] = idx.matching(qt, st, ger)
		qPages = qPages[:0]
		for _, t := range matches[qi] {
			e := idx.terms[t]
			mult := multiplier(t, qt, ger)
			whole := t == qt || t == st || t == ger
			tPages = tPages[:0]
			for _, p := range e.postings {
				if matched[p.page] != qi {
					continue
				}
				v := e.idf * mult * int64(p.score)
				if sums[p.page] == 0 && best[p.page] == 0 {
					tPages = append(tPages, p.page)
				}
				sums[p.page] += v
				best[p.page] = max(best[p.page], v)
				if p.tLen > 0 {
					titled[p.page] = true
					if slots[p.page] == nil {
						slots[p.page] = map[int]int64{}
					}
					sl := slots[p.page]
					if whole {
						for s := p.tFrom; s < p.tFrom+p.tLen; s++ {
							sl[s] = 2
						}
					} else {
						sl[p.tFrom] = max(sl[p.tFrom], 1)
					}
				}
			}
			for _, pg := range tPages {
				if perPage[pg] == 0 {
					qPages = append(qPages, pg)
				}
				perPage[pg] = max(perPage[pg], best[pg]+(sums[pg]-best[pg])/4)
				sums[pg], best[pg] = 0, 0
			}
		}
		for _, pg := range qPages {
			pageScore[pg] += perPage[pg]
			matched[pg]++
			if titled[pg] {
				inTitle[pg]++
			}
			perPage[pg], titled[pg] = 0, false
		}
		if len(qPages) == 0 {
			return nil, 0
		}
	}
	var order []int
	for pg := range n {
		if matched[pg] != len(terms) {
			continue
		}
		order = append(order, pg)
		if sn := int64(idx.titleN[pg]); inTitle[pg] == len(terms) && sn > 0 {
			var pts int64
			for _, v := range slots[pg] {
				pts += v
			}
			pageScore[pg] += titleBonus * pts / (2 * sn)
		}
	}
	slices.SortFunc(order, func(a, b int) int {
		if c := cmp.Compare(pageScore[b], pageScore[a]); c != 0 {
			return c
		}
		return cmp.Compare(a, b)
	})
	total := len(order)
	start := min(offset, total)
	order = order[start : start+min(limit, total-start)]
	secScore := idx.sectionScores(order, terms, matches)
	out := make([]Result, 0, len(order))
	for _, pg := range order {
		p := idx.pages[pg]
		r := Result{URL: p.URL, Title: p.Title, Group: p.Group, Score: pageScore[pg]}
		var hits []int
		for si, s := range p.Sections {
			if si > 0 && s.ID != "" && secScore[sectionKey{pg, si}] > 0 {
				hits = append(hits, si)
			}
		}
		slices.SortStableFunc(hits, func(a, b int) int {
			return cmp.Compare(secScore[sectionKey{pg, b}], secScore[sectionKey{pg, a}])
		})
		if len(hits) > maxSectionHits {
			hits = hits[:maxSectionHits]
		}
		for _, si := range hits {
			s := p.Sections[si]
			r.Sections = append(r.Sections, SectionHit{ID: s.ID, Heading: s.Heading, Excerpt: excerpt(s.Text, terms)})
		}
		if secScore[sectionKey{pg, 0}] > 0 || len(hits) == 0 {
			r.Excerpt = excerpt(p.Sections[0].Text, terms)
		}
		out = append(out, r)
	}
	if len(out) == 0 {
		return nil, total
	}
	return out, total
}

type sectionKey struct{ page, section int }

func multiplier(t, qt, ger string) int64 {
	if t == qt || t == ger {
		return 2
	}
	return 1
}

func (idx *Index) sectionScores(pages []int, terms []string, matches [][]string) map[sectionKey]int64 {
	out := map[sectionKey]int64{}
	for qi, qt := range terms {
		ger := gerund(qt)
		per := map[sectionKey]int64{}
		for _, t := range matches[qi] {
			e := idx.terms[t]
			mult := multiplier(t, qt, ger)
			for _, pg := range pages {
				i, _ := slices.BinarySearchFunc(e.postings, pg, func(p posting, pg int) int { return cmp.Compare(p.page, pg) })
				for ; i < len(e.postings) && e.postings[i].page == pg; i++ {
					p := e.postings[i]
					k := sectionKey{pg, p.section}
					per[k] = max(per[k], e.idf*mult*int64(p.score))
				}
			}
		}
		for k, v := range per {
			out[k] += v
		}
	}
	return out
}

func stem(q string) string {
	rs := []rune(q)
	if len(rs) < 4 || rs[len(rs)-1] != 's' || rs[len(rs)-2] == 's' {
		return ""
	}
	return string(rs[:len(rs)-1])
}

func gerund(q string) string {
	rs := []rune(q)
	if len(rs) < 4 || stem(q) != "" || strings.HasSuffix(q, "ing") {
		return ""
	}
	if n := len(rs); rs[n-1] == 'e' && rs[n-2] != 'e' {
		return string(rs[:n-1]) + "ing"
	}
	return q + "ing"
}

const (
	excerptLead  = 40
	excerptWidth = 160
)

func excerpt(text string, terms []string) []Segment {
	rs := []rune(text)
	var hits []token
	anchor := -1
	for _, t := range scan(text) {
		hit, whole := false, false
		for _, q := range terms {
			st, ger := stem(q), gerund(q)
			if t.word == q || prefixes(q) && strings.HasPrefix(t.word, q) || st != "" && strings.HasPrefix(t.word, st) || ger != "" && t.word == ger {
				hit = true
				whole = whole || t.word == q || t.word == st || t.word == ger
			}
		}
		if !hit {
			continue
		}
		if anchor < 0 && whole {
			anchor = len(hits)
		}
		hits = append(hits, t)
	}
	if anchor < 0 && len(hits) > 0 {
		anchor = 0
	}
	start := 0
	if anchor >= 0 && hits[anchor].start > excerptLead {
		a := hits[anchor].start
		start = a - excerptLead
		if i := indexRune(rs[start:a], ' '); i >= 0 {
			start += i + 1
		} else {
			start = a
		}
	}
	end := min(len(rs), start+excerptWidth)
	if end < len(rs) {
		from := start
		if anchor >= 0 {
			from = min(hits[anchor].end, end)
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

func prefixes(q string) bool { return utf8.RuneCountInString(q) >= minPrefixRunes }
