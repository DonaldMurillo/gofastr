package sitesearch

import (
	"bytes"
	"compress/gzip"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// bigCorpus builds n pages from the vocabulary of the framework's own
// docs, real technical English: each word is drawn as often as the
// docs use it, so prefixes spread the way a docs site's do. Each page
// has six sections of 130 words and a camelCase title.
func bigCorpus(tb testing.TB, n int) []Page {
	tb.Helper()
	paths, err := filepath.Glob("../../framework/docs/content/*.md")
	if err != nil || len(paths) == 0 {
		tb.Fatalf("docs corpus: %v (%d files)", err, len(paths))
	}
	freq := map[string]int{}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			tb.Fatal(err)
		}
		for _, w := range strings.FieldsFunc(strings.ToLower(string(b)), func(r rune) bool { return r < 'a' || r > 'z' }) {
			if len(w) >= minTermRunes {
				freq[w]++
			}
		}
	}
	vocab := slices.Sorted(maps.Keys(freq))
	slices.SortStableFunc(vocab, func(a, b string) int { return freq[b] - freq[a] })
	cum := make([]int, len(vocab))
	total := 0
	for i, w := range vocab {
		total += freq[w]
		cum[i] = total
	}
	rng := rand.New(rand.NewPCG(1, 2))
	draw := func() string {
		i, _ := slices.BinarySearch(cum, 1+rng.IntN(total))
		return vocab[i]
	}
	words := func(k int) string {
		out := make([]string, k)
		for i := range out {
			out[i] = draw()
		}
		return strings.Join(out, " ")
	}
	pages := make([]Page, n)
	for i := range pages {
		a, b := vocab[50+rng.IntN(3000)], vocab[50+rng.IntN(3000)]
		title := strings.ToUpper(a[:1]) + a[1:] + strings.ToUpper(b[:1]) + b[1:]
		secs := make([]Section, 6)
		for j := range secs {
			secs[j] = Section{Text: words(130)}
			if j > 0 {
				secs[j].ID, secs[j].Heading = fmt.Sprintf("s%d", j), words(3)
			}
		}
		pages[i] = Page{URL: fmt.Sprintf("/p/%d", i), Title: title, Sections: secs}
	}
	return pages
}

func gzLen(b []byte) int {
	var buf bytes.Buffer
	w, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	_, _ = w.Write(b)
	_ = w.Close()
	return buf.Len()
}

// fetched lists the files the static client downloads for q: the
// manifest, the shards its terms read, and the page files of the top
// results.
func fetched(t *testing.T, idx *Index, files map[string][]byte, q string) []string {
	t.Helper()
	var m manifest
	if err := json.Unmarshal(files["manifest.json"], &m); err != nil {
		t.Fatal(err)
	}
	out := []string{"manifest.json"}
	for _, term := range queryTerms(q) {
		out = append(out, shardsFor(m, term)...)
	}
	pageOf := map[string]int{}
	for i, p := range idx.pages {
		pageOf[p.URL] = i
	}
	for _, r := range idx.Search(q, defaultLimit) {
		out = append(out, pageFile(m, pageOf[r.URL]))
	}
	seen := map[string]bool{}
	var uniq []string
	for _, f := range out {
		if !seen[f] {
			seen[f] = true
			uniq = append(uniq, f)
		}
	}
	return uniq
}

// The export stays small as a site grows: at 10,000 pages the first
// query downloads a small manifest, small shards and the top pages,
// never a share of the index that grows with the site.
func TestExportStaysSmall(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a 10,000-page index")
	}
	idx := Build(bigCorpus(t, 10000))
	files, err := idx.Files()
	if err != nil {
		t.Fatal(err)
	}
	const (
		manifestBudget = 30 << 10
		shardBudget    = 50 << 10
		queryBudget    = 150 << 10
	)
	if n := gzLen(files["manifest.json"]); n > manifestBudget {
		t.Errorf("manifest: %d B gzipped, budget %d", n, manifestBudget)
	}
	biggest, name := 0, ""
	for f, body := range files {
		if strings.HasPrefix(f, "t-") {
			if n := gzLen(body); n > biggest {
				biggest, name = n, f
			}
		}
	}
	t.Logf("%d files; largest shard %s %d B gz; manifest %d B gz", len(files), name, biggest, gzLen(files["manifest.json"]))
	if biggest > shardBudget {
		t.Errorf("largest shard %s: %d B gzipped, budget %d", name, biggest, shardBudget)
	}
	for _, q := range []string{"form", "pro", "con", "re", "co", "st", "pa", "valid", "migration", "the", "session token"} {
		total := 0
		for _, f := range fetched(t, idx, files, q) {
			total += gzLen(files[f])
		}
		t.Logf("%q downloads %d B gz", q, total)
		if total > queryBudget {
			t.Errorf("%q downloads %d B gzipped, budget %d", q, total, queryBudget)
		}
	}
}

func BenchmarkBuild(b *testing.B) {
	pages := bigCorpus(b, 10000)
	b.ResetTimer()
	for b.Loop() {
		Build(pages)
	}
}

func BenchmarkSearch(b *testing.B) {
	idx := Build(bigCorpus(b, 10000))
	for _, q := range []string{"co", "pro", "form", "session token"} {
		b.Run(q, func(b *testing.B) {
			for b.Loop() {
				idx.Search(q, defaultLimit)
			}
		})
	}
}

// shardsFor and pageFile resolve names the way sitesearch-static.js
// does. A term that matches as a prefix reads every shard whose key
// begins with its match prefix, and the shard whose key the prefix
// begins with (a key that never split holds every term under it). A
// shorter term reads only that second shard, and a gerund is one more
// exact term.
func shardsFor(m manifest, term string) []string {
	from := term
	if st := stem(term); st != "" {
		from = st
	}
	var out []string
	for _, k := range m.Keys {
		if strings.HasPrefix(from, k) || prefixes(term) && strings.HasPrefix(k, from) {
			out = append(out, "t-"+hex.EncodeToString([]byte(k))+"-"+m.Hash+".json")
		}
	}
	if g := gerund(term); g != "" {
		for _, k := range m.Keys {
			if strings.HasPrefix(g, k) && !strings.HasPrefix(from, k) && !strings.HasPrefix(k, from) {
				out = append(out, "t-"+hex.EncodeToString([]byte(k))+"-"+m.Hash+".json")
			}
		}
	}
	return out
}

func pageFile(m manifest, page int) string {
	return "p" + strconv.Itoa(page) + "-" + m.Hash + ".json"
}
