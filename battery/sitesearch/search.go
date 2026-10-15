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
	// Per page, how many query terms its title holds and the points
	// each title slot earned (titleBonus).
	inTitle, slots := map[int]int{}, map[int]map[int]int64{}
	for qi, qt := range terms {
		perPage := map[int]int64{}
		perSec := map[key]int64{}
		titled := map[int]bool{}
		st, ger := stem(qt), gerund(qt)
		for _, t := range idx.matching(qt, st, ger) {
			e := idx.terms[t]
			mult := int64(1)
			if t == qt || t == ger {
				mult = 2
			}
			// A title term the query names whole, as the singular of
			// its plural or in its -ing form, fills its slots; one it
			// only begins earns its first slot one point.
			whole := t == qt || t == st || t == ger
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
		for pg := range titled {
			inTitle[pg]++
		}
		if len(pageScore) == 0 {
			return nil
		}
	}
	for pg := range pageScore {
		if n := int64(idx.titleN[pg]); inTitle[pg] == len(terms) && n > 0 {
			var pts int64
			for _, v := range slots[pg] {
				pts += v
			}
			pageScore[pg] += titleBonus * pts / (2 * n)
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

// gerund is the -ing form a query term also matches as a whole word,
// so "theme" finds Theming and "cache" caching: the term with a final
// lone e dropped, plus ing, for terms of four or more runes that are
// neither plural nor -ing already. Empty when there is none. It keeps
// the term's first two runes, so it lives in the term's shard.
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

// Excerpt window, in runes: the window opens up to excerptLead runes
// before the first hit and spans excerptWidth.
const (
	excerptLead  = 40
	excerptWidth = 160
)

// excerpt cuts the window of text around the first word a query term
// names whole (or, failing that, the first it begins) and splits it
// into plain and hit runs; a plural term also marks its singular, and
// a term its -ing form. The
// window's edges snap to spaces so no word is cut: with no space in
// the lead-in (a long URL), it opens on the hit. An ellipsis marks a
// cut end.
func excerpt(text string, terms []string) []Segment {
	rs := []rune(text)
	var hits []token
	anchor := -1
	for _, t := range scan(text) {
		hit, whole := false, false
		for _, q := range terms {
			st, ger := stem(q), gerund(q)
			if strings.HasPrefix(t.word, q) || st != "" && strings.HasPrefix(t.word, st) || ger != "" && t.word == ger {
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
