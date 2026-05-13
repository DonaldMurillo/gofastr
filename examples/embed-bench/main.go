// Package main is a benchmark comparing two ways to "find things in
// this repo" — the way a Claude Code-style agent does it (shell out
// to ripgrep with carefully chosen keywords) versus the way
// battery/embed does it (one natural-language query against a
// persisted semantic index).
//
// Run it after indexing the repo:
//
//	ollama serve &  # or: brew services start ollama
//	ollama pull nomic-embed-text
//	EMBED_BACKEND=ollama go run ./cmd/gofastr embed index ./battery ./docs ./core ./framework ./kiln
//	go run ./examples/embed-bench
//
// What it asserts:
//
//   - Time-to-answer per method (wall clock).
//   - Did the "right" file appear at all? (For ripgrep, in the match
//     list — order is by file, not by relevance, so we report
//     match-list size.) For embed, the rank of the target in top-5.
//
// The comparison is intentionally generous to ripgrep: the keyword
// list per query is the *best* set of literal terms a human (or a
// language model) would pick, not a naive word split. This is what
// Claude Code actually does at runtime — it doesn't grep the
// verbatim question, it picks a tight token query first.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/embed"
)

type query struct {
	name        string   // human-readable label
	natural     string   // the question as a user would phrase it
	rgKeywords  []string // the tight keyword set a human/agent would actually grep for
	rgPaths     []string // path scope passed to rg (mirrors what we index)
	targetGlob  string   // a substring the right file's path contains
	description string   // what we're testing
}

func main() {
	queries := []query{
		{
			name:        "auth: middleware",
			natural:     "how do I add authentication middleware to my routes",
			rgKeywords:  []string{"auth", "middleware"},
			targetGlob:  "battery/auth/session_middleware.go",
			description: "concept query — exact-keyword path exists but conceptual phrasing differs",
		},
		{
			name:        "cache: TTL eviction",
			natural:     "how does the cache decide when an entry has expired",
			rgKeywords:  []string{"cache", "TTL"},
			targetGlob:  "battery/cache/",
			description: "near-keyword: 'expired' is in the query, code uses 'TTL/Expires'",
		},
		{
			name:        "SSE: server-pushed updates",
			natural:     "stream events from the server to the browser so it updates without polling",
			rgKeywords:  []string{"SSE", "EventSource"},
			targetGlob:  "events.md",
			description: "paraphrase: 'stream events' / 'no polling' vs literal 'SSE'",
		},
		{
			name:        "embed: how does RRF fusion work",
			natural:     "how do you combine vector retrieval with keyword search",
			rgKeywords:  []string{"fuseRRF", "ReciprocalRank"},
			targetGlob:  "battery/embed/hybrid.go",
			description: "internal API — grep wins iff you already know the symbol name",
		},
		{
			name:        "kiln: tool dispatch",
			natural:     "where does the agent loop dispatch tool calls",
			rgKeywords:  []string{"dispatch", "ToolCall"},
			targetGlob:  "kiln/agent/loop.go",
			description: "concept query with overlapping vocab",
		},
		{
			name:        "migrate: schema diff",
			natural:     "how does the framework figure out what columns to add when an entity changes",
			rgKeywords:  []string{"SchemaDiff", "diff"},
			targetGlob:  "framework/migrate/",
			description: "long concept query — grep keywords are sparse, embed should do better",
		},
		{
			name:        "openapi: route generation",
			natural:     "how do entity declarations get turned into OpenAPI operations",
			rgKeywords:  []string{"openapi", "operation"},
			targetGlob:  "framework/openapi/",
			description: "two-step concept (entity → openapi)",
		},
	}

	idxScope := []string{"./battery", "./docs", "./core", "./framework", "./kiln"}

	results := make([]row, len(queries))
	for i, q := range queries {
		results[i] = runQuery(q, idxScope)
	}

	printTable(queries, results)
	printSummary(queries, results)
}

type row struct {
	rgTime       time.Duration
	rgMatches    int
	rgFoundTarget bool
	embedTime     time.Duration
	embedHits     []embed.Hit
	embedRank     int // 0-based; -1 if not in top-K
	embedErr      error
	rgErr         error
}

func runQuery(q query, scope []string) row {
	var r row

	// --- ripgrep arm ----------------------------------------------------
	// Equivalent to: rg -l --hidden --glob '!.git' '<kw1>|<kw2>' <paths...>
	// -l prints just file paths so we measure search, not output rendering.
	pattern := strings.Join(q.rgKeywords, "|")
	args := []string{"-l", "--hidden", "--glob", "!.git", "--glob", "!.gofastr", "--glob", "!dist", "-e", pattern}
	args = append(args, scope...)

	rgStart := time.Now()
	out, err := exec.Command("rg", args...).Output()
	r.rgTime = time.Since(rgStart)
	if err != nil {
		// rg exits 1 when no matches; treat as zero matches, not error.
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			err = nil
		} else {
			r.rgErr = err
		}
	}
	matches := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(matches) == 1 && matches[0] == "" {
		matches = nil
	}
	r.rgMatches = len(matches)
	for _, m := range matches {
		if strings.Contains(m, q.targetGlob) {
			r.rgFoundTarget = true
			break
		}
	}

	// --- embed arm ------------------------------------------------------
	// We talk to the gofastr CLI's HTTP shape if a GOFASTR_URL is set;
	// otherwise we open the local snapshot directly. This bench shells
	// to the CLI for parity with how a user would invoke it.
	embStart := time.Now()
	hits, err := callEmbedCLI(q.natural, 5)
	r.embedTime = time.Since(embStart)
	if err != nil {
		r.embedErr = err
	}
	r.embedHits = hits
	r.embedRank = -1
	for i, h := range hits {
		src := h.Chunk.Source
		if strings.Contains(src, q.targetGlob) {
			r.embedRank = i
			break
		}
	}
	return r
}

// callEmbedCLI runs `gofastr embed query` with EMBED_BACKEND=ollama so
// the benchmark exercises the same code path a user does. We could
// link battery/embed directly, but that would hide the per-process
// snapshot-load cost which matters for "is this fast in practice?".
func callEmbedCLI(text string, k int) ([]embed.Hit, error) {
	// Prefer GOFASTR_URL if set (no per-call snapshot load).
	if url := os.Getenv("GOFASTR_URL"); url != "" {
		body, _ := json.Marshal(embed.Query{Text: text, K: k, Hybrid: true})
		resp, err := http.Post(strings.TrimRight(url, "/")+"/embed/query", "application/json", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		var out struct{ Hits []embed.Hit }
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			return nil, err
		}
		return out.Hits, nil
	}
	// Local CLI path. The repo build of gofastr lives at dist/ after
	// `make build`; fall back to `go run` if no binary is around.
	bin := findGofastrBin()
	args := []string{"embed", "query", text, "-k", fmt.Sprintf("%d", k), "--hybrid"}
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), "EMBED_BACKEND=ollama")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		// The CLI prints failure messages to stdout (via its fail()
		// helper) before exiting non-zero. Surface that too so the
		// bench is debuggable.
		return nil, fmt.Errorf("gofastr embed query: %w\nstdout: %s\nstderr: %s", err, string(out), stderr.String())
	}
	var hits []embed.Hit
	if err := json.Unmarshal(out, &hits); err != nil {
		return nil, fmt.Errorf("decode hits: %w", err)
	}
	return hits, nil
}

// findGofastrBin returns an absolute path to the gofastr binary. It
// prefers the freshly-built dist/gofastr (via `make build`) over any
// stray /tmp/gofastr — an out-of-date binary in /tmp produced
// "unknown command: embed" errors that wasted a debugging cycle.
func findGofastrBin() string {
	type cand struct {
		path string
		info os.FileInfo
	}
	var found []cand
	for _, p := range []string{"./dist/gofastr", "/tmp/gofastr"} {
		fi, err := os.Stat(p)
		if err == nil {
			found = append(found, cand{p, fi})
		}
	}
	if len(found) == 0 {
		fmt.Fprintln(os.Stderr, "embed-bench: no gofastr binary found at ./dist/gofastr or /tmp/gofastr — run `make build` first")
		os.Exit(2)
	}
	// Pick the newest. Stale binaries are the #1 footgun here.
	newest := found[0]
	for _, f := range found[1:] {
		if f.info.ModTime().After(newest.info.ModTime()) {
			newest = f
		}
	}
	abs, _ := filepath.Abs(newest.path)
	return abs
}

func printTable(queries []query, results []row) {
	fmt.Printf("\n%-32s  %-9s  %-7s  %-6s  %-9s  %-12s\n",
		"query", "rg time", "rg hits", "rg ✓?", "embed t", "embed rank")
	fmt.Println(strings.Repeat("-", 88))
	for i, q := range queries {
		r := results[i]
		rgCheck := "✗"
		if r.rgFoundTarget {
			rgCheck = "✓"
		}
		if r.rgErr != nil {
			rgCheck = "ERR"
		}
		embedRank := "-"
		if r.embedRank >= 0 {
			embedRank = fmt.Sprintf("%d ✓", r.embedRank+1)
		} else if r.embedErr != nil {
			embedRank = "ERR"
		} else {
			embedRank = "miss"
		}
		fmt.Printf("%-32s  %-9s  %-7d  %-6s  %-9s  %-12s\n",
			truncate(q.name, 32),
			fmtDur(r.rgTime), r.rgMatches, rgCheck,
			fmtDur(r.embedTime), embedRank)
	}

	// For every embed miss, print the actual top-3 hits so the reader
	// can decide whether the embedder was wrong or our targetGlob was.
	fmt.Println()
	for i, q := range queries {
		r := results[i]
		if r.embedRank >= 0 || r.embedErr != nil {
			continue
		}
		fmt.Printf("embed missed %q — target %q. Top-3 returned:\n", q.name, q.targetGlob)
		for j, h := range r.embedHits {
			if j >= 3 {
				break
			}
			src := strings.TrimPrefix(h.Chunk.Source, mustGetwd()+"/")
			fmt.Printf("  %d. %s   (score=%.4f)\n", j+1, src, h.Score)
		}
		fmt.Println()
	}
}

func mustGetwd() string {
	wd, _ := os.Getwd()
	return wd
}

func printSummary(queries []query, results []row) {
	var rgWins, embedWins, both, neither int
	var rgTotal, embedTotal time.Duration
	for _, r := range results {
		switch {
		case r.rgFoundTarget && r.embedRank >= 0:
			both++
		case r.rgFoundTarget:
			rgWins++
		case r.embedRank >= 0:
			embedWins++
		default:
			neither++
		}
		rgTotal += r.rgTime
		embedTotal += r.embedTime
	}
	n := len(results)
	fmt.Println()
	fmt.Println(strings.Repeat("-", 88))
	fmt.Printf("queries:                 %d\n", n)
	fmt.Printf("both found target:       %d\n", both)
	fmt.Printf("only ripgrep found:      %d\n", rgWins)
	fmt.Printf("only embed found:        %d\n", embedWins)
	fmt.Printf("neither found:           %d\n", neither)
	if n > 0 {
		fmt.Printf("avg ripgrep time:        %s\n", fmtDur(rgTotal/time.Duration(n)))
		fmt.Printf("avg embed query time:    %s\n", fmtDur(embedTotal/time.Duration(n)))
	}
	fmt.Println()
	fmt.Println("notes:")
	fmt.Println("  - ripgrep keyword set is the *best* set of literal terms a human")
	fmt.Println("    would pick — what Claude Code does at runtime when it picks tokens")
	fmt.Println("    before grepping. Naive whole-question grep is much worse.")
	fmt.Println("  - embed is a single natural-language call, no per-query tuning.")
	fmt.Println("  - rg \"hits\" is the count of matched files. The user still has to")
	fmt.Println("    open them; embed returns ranked chunks with their text inline.")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func fmtDur(d time.Duration) string {
	if d > time.Second {
		return fmt.Sprintf("%.2fs", d.Seconds())
	}
	return fmt.Sprintf("%dms", d.Milliseconds())
}

