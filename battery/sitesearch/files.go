package sitesearch

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
)

// The static index layout, under Root:
//
//	manifest.json        {"v":1,"shards":{"pa":"t-7061-<hash>.json",…},"pages":["p0-<hash>.json",…]}
//	t-<hex>-<hash>.json  the terms sharing a two-rune prefix:
//	                     {"palette":[idf, page,section,score, page,section,score, …],…}
//	p<n>-<hash>.json     one page: {"u":url,"t":title,"g":group,"s":[[id,heading,text],…]}
//
// A query term reads one shard (its first two runes name it), so a
// search downloads the shards of its terms and then the fragments of
// the pages it shows, never the whole index. Every name but the
// manifest carries a content hash, so a host may cache them forever.

// Root is the URL path the index files are served and exported under.
const Root = "/__gofastr/search/"

// formatVersion is the manifest's "v"; the client refuses another.
const formatVersion = 1

type manifest struct {
	V      int               `json:"v"`
	Shards map[string]string `json:"shards"`
	Pages  []string          `json:"pages"`
}

type fragment struct {
	URL      string      `json:"u"`
	Title    string      `json:"t"`
	Group    string      `json:"g,omitempty"`
	Sections [][3]string `json:"s"`
}

// Files renders the index as the static files the browser client
// reads, keyed by file name relative to Root.
func (idx *Index) Files() (map[string][]byte, error) {
	files := map[string][]byte{}
	m := manifest{V: formatVersion, Shards: map[string]string{}}

	shards := map[string]map[string][]int64{}
	for _, t := range idx.keys {
		k := shardKey(t)
		if shards[k] == nil {
			shards[k] = map[string][]int64{}
		}
		e := idx.terms[t]
		row := make([]int64, 0, 1+3*len(e.postings))
		row = append(row, e.idf)
		for _, p := range e.postings {
			row = append(row, int64(p.page), int64(p.section), int64(p.score))
		}
		shards[k][t] = row
	}
	for _, k := range slices.Sorted(maps.Keys(shards)) {
		body, err := json.Marshal(shards[k])
		if err != nil {
			return nil, fmt.Errorf("sitesearch: shard %q: %w", k, err)
		}
		name := "t-" + hex.EncodeToString([]byte(k)) + "-" + contentHash(body) + ".json"
		files[name] = body
		m.Shards[k] = name
	}

	for i, p := range idx.pages {
		f := fragment{URL: p.URL, Title: p.Title, Group: p.Group, Sections: make([][3]string, len(p.Sections))}
		for j, s := range p.Sections {
			f.Sections[j] = [3]string{s.ID, s.Heading, s.Text}
		}
		body, err := json.Marshal(f)
		if err != nil {
			return nil, fmt.Errorf("sitesearch: page %q: %w", p.URL, err)
		}
		name := "p" + strconv.Itoa(i) + "-" + contentHash(body) + ".json"
		files[name] = body
		m.Pages = append(m.Pages, name)
	}

	body, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("sitesearch: manifest: %w", err)
	}
	files["manifest.json"] = body
	return files, nil
}

// shardKey is the first two runes of a term: the shard it lives in.
func shardKey(t string) string {
	n := 0
	for i := range t {
		if n == minTermRunes {
			return t[:i]
		}
		n++
	}
	return t
}

func contentHash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:4])
}
