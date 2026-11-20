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

// Root is the URL path the index files are served and exported under.
const Root = "/__gofastr/search/"

const formatVersion = 3

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

// Files renders the index as the static files the browser client reads,
// keyed by file name relative to Root.
func (idx *Index) Files() (map[string][]byte, error) { return idx.files(shardMax) }

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

func writeLen(h hash.Hash, s string) {
	var n [8]byte
	binary.BigEndian.PutUint64(n[:], uint64(len(s)))
	h.Write(n[:])
	h.Write([]byte(s))
}

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
