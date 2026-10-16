package sitesearch

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"maps"
	"slices"
	"strconv"
	"unicode/utf8"
)

// The static index layout, under Root:
//
//	manifest.json        {"v":3,"h":"<hash>","keys":["co","con","pa",…]}
//	t-<hex>-<hash>.json  the terms under one key, each a row of integers:
//	                     {"palette":[idf, n, pages…, sections…, scores…, titles…],…}
//	p<n>-<hash>.json     one page: {"u":url,"t":title,"g":group,"s":[[id,heading,text],…]}
//
// A shard holds the terms that begin with its key, hex-encoded in its
// name. A key starts as a term's first two runes, and a shard over
// shardMax bytes splits on the next rune, so a site's common prefixes
// (re, co) cost no more to fetch than its rare ones; a term as long as
// a split key stays in that key's shard. A query term reads the shards
// its matches can live in (shardsFor in the client), then the files of
// the pages it shows, never the whole index.
//
// A row lists a term's n postings in page order as three columns: page
// numbers as gaps from the one before (the first from zero), section
// numbers and scores. Then, for each posting on a page's first section
// whose title holds the term, four numbers: the posting's place in the
// row, the title slots it names (from, count) and the page's slot
// count (titleBonus).
//
// Every file but the manifest carries the hash of the whole index:
// any content change moves every term's idf, so no file outlives a
// rebuild anyway, and one hash keeps the manifest to the shard keys.
// A host may cache those files forever.

// Root is the URL path the index files are served and exported under.
const Root = "/__gofastr/search/"

// formatVersion is the manifest's "v"; the client refuses another.
const formatVersion = 3

// shardMax is the size over which a shard splits on its next rune.
const shardMax = 64 << 10

type manifest struct {
	V    int      `json:"v"`
	Hash string   `json:"h"`
	Keys []string `json:"keys"`
}

type fragment struct {
	URL      string      `json:"u"`
	Title    string      `json:"t"`
	Group    string      `json:"g,omitempty"`
	Sections [][3]string `json:"s"`
}

// Files renders the index as the static files the browser client
// reads, keyed by file name relative to Root.
func (idx *Index) Files() (map[string][]byte, error) { return idx.files(shardMax) }

// files renders the index with shards split over max bytes.
func (idx *Index) files(max int) (map[string][]byte, error) {
	rows := make(map[string][]byte, len(idx.keys))
	for _, t := range idx.keys {
		body, err := json.Marshal(idx.row(t))
		if err != nil {
			return nil, fmt.Errorf("sitesearch: term %q: %w", t, err)
		}
		rows[t] = body
	}
	shards := splitShards(idx.keys, rows, max)
	keys := slices.Sorted(maps.Keys(shards))
	bodies := map[string][]byte{}
	sum := sha256.New()
	for _, k := range keys {
		var buf bytes.Buffer
		buf.WriteByte('{')
		for i, t := range shards[k] {
			if i > 0 {
				buf.WriteByte(',')
			}
			key, err := json.Marshal(t)
			if err != nil {
				return nil, fmt.Errorf("sitesearch: term %q: %w", t, err)
			}
			buf.Write(key)
			buf.WriteByte(':')
			buf.Write(rows[t])
		}
		buf.WriteByte('}')
		bodies["t-"+hex.EncodeToString([]byte(k))] = buf.Bytes()
		writeLen(sum, k)
		writeLen(sum, buf.String())
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
		bodies["p"+strconv.Itoa(i)] = body
		writeLen(sum, string(body))
	}
	hash := hex.EncodeToString(sum.Sum(nil)[:4])
	files := make(map[string][]byte, len(bodies)+1)
	for stem, body := range bodies {
		files[stem+"-"+hash+".json"] = body
	}
	body, err := json.Marshal(manifest{V: formatVersion, Hash: hash, Keys: keys})
	if err != nil {
		return nil, fmt.Errorf("sitesearch: manifest: %w", err)
	}
	files["manifest.json"] = body
	return files, nil
}

// writeLen writes s to h length-prefixed, so no two different file
// sets hash the same bytes.
func writeLen(h hash.Hash, s string) {
	var n [8]byte
	binary.BigEndian.PutUint64(n[:], uint64(len(s)))
	h.Write(n[:])
	h.Write([]byte(s))
}

// row encodes a term's postings as the static files carry them.
func (idx *Index) row(t string) []int64 {
	ps := idx.terms[t].postings
	n := len(ps)
	row := make([]int64, 2+3*n, 2+3*n+4)
	row[0], row[1] = idx.terms[t].idf, int64(n)
	prev := 0
	for i, p := range ps {
		row[2+i] = int64(p.page - prev)
		prev = p.page
		row[2+n+i] = int64(p.section)
		row[2+2*n+i] = int64(p.score)
	}
	for i, p := range ps {
		if p.tLen > 0 {
			row = append(row, int64(i), int64(p.tFrom), int64(p.tLen), int64(idx.titleN[p.page]))
		}
	}
	return row
}

// splitShards groups the sorted terms into shards by key: a term's
// first two runes, then one rune more for as long as the group's rows
// are over max bytes. Terms as long as a split key stay in its shard.
func splitShards(keys []string, rows map[string][]byte, max int) map[string][]string {
	out := map[string][]string{}
	var place func(key string, terms []string, depth int)
	place = func(key string, terms []string, depth int) {
		size := 0
		for _, t := range terms {
			size += len(t) + len(rows[t]) + 4
		}
		var rest []string
		for _, t := range terms {
			if size <= max || utf8.RuneCountInString(t) <= depth {
				out[key] = append(out[key], t)
			} else {
				rest = append(rest, t)
			}
		}
		for i := 0; i < len(rest); {
			k := runePrefix(rest[i], depth+1)
			j := i + 1
			for j < len(rest) && runePrefix(rest[j], depth+1) == k {
				j++
			}
			place(k, rest[i:j], depth+1)
			i = j
		}
	}
	for i := 0; i < len(keys); {
		k := runePrefix(keys[i], minTermRunes)
		j := i + 1
		for j < len(keys) && runePrefix(keys[j], minTermRunes) == k {
			j++
		}
		place(k, keys[i:j], minTermRunes)
		i = j
	}
	return out
}

// runePrefix is the first n runes of t, or all of t.
func runePrefix(t string, n int) string {
	i := 0
	for j := range t {
		if i == n {
			return t[:j]
		}
		i++
	}
	return t
}
