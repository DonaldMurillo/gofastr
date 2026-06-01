package main

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/embed"
)

// runEmbed dispatches `gofastr embed <subcommand>`.
//
//   - index <path...>     One-shot: walk the path(s) and add every
//                         matching file to the local index.
//   - watch <path...>     Index, then poll for changes until SIGINT.
//   - query <text>        Print top-K hits as JSON.
//   - stats               Print index stats as JSON.
//   - clear               Delete the local snapshot + WAL for this cwd.
//
// When the GOFASTR_URL environment variable is set, query and stats
// hit that server's /embed/* endpoints instead of opening a local
// index. index/watch/clear are always local — long-lived mutations
// should run alongside the server, not through it.
func runEmbed(args []string) {
	if len(args) == 0 {
		printEmbedHelp()
		return
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "index":
		embedIndex(rest, false)
	case "watch":
		embedIndex(rest, true)
	case "query", "q":
		embedQuery(rest)
	case "stats":
		embedStats()
	case "clear":
		embedClear()
	case "help", "--help", "-h":
		printEmbedHelp()
	default:
		fail("unknown embed subcommand: %s", sub)
		printEmbedHelp()
		osExit(1)
	}
}

func printEmbedHelp() {
	fmt.Printf(`
%s — semantic search for the project at cwd

%s:
  gofastr embed <subcommand> [args]

%s:
  index <path...>     One-shot index of the given paths
  watch <path...>     Index then re-scan on a 2s timer until SIGINT
  query "<text>"      Print top hits as JSON
  stats               Print index stats as JSON
  clear               Delete the local snapshot for this directory

%s:
  GOFASTR_URL=...     If set, query/stats hit the running server's
                      /embed/* endpoints instead of opening a local
                      index. index/watch/clear are always local.
  EMBED_BACKEND=ollama  Use the Ollama HTTP embedder instead of the
                      built-in stub. Set OLLAMA_URL and OLLAMA_MODEL
                      to override defaults (localhost:11434 +
                      nomic-embed-text).

%s:
  gofastr embed index .
  gofastr embed watch ./src ./docs
  gofastr embed query "authentication middleware"
`, bold("gofastr embed"), bold("Usage"), bold("Subcommands"), bold("Environment"), bold("Examples"))
}

// localSnapshotDir returns ~/.gofastr/embed/<cwd-hash>. Distinct cwds
// get distinct snapshot directories so we don't index different
// projects into the same file.
func localSnapshotDir() string {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "unknown"
	}
	h := sha1.Sum([]byte(cwd))
	tag := hex.EncodeToString(h[:6])
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".gofastr", "embed", tag)
}

// openLocalIndex constructs an Index. The embedder is chosen by
// environment:
//
//   - EMBED_BACKEND=ollama  → OllamaEmbedder (real semantic, requires
//     a running Ollama-compatible server)
//   - anything else         → StubEmbedder (deterministic, no setup,
//     low retrieval quality — fine for dev/tests)
//
// Additional env knobs for the Ollama path:
//
//   - OLLAMA_URL    (default http://localhost:11434)
//   - OLLAMA_MODEL  (default nomic-embed-text)
func openLocalIndex() (embed.Index, error) {
	idx, err := embed.Open(embed.Options{
		Embedder: chooseEmbedder(),
		Keyword:  embed.NewMemoryKeyword(),
		Path:     localSnapshotDir(),
	})
	return idx, err
}

func chooseEmbedder() embed.Embedder {
	if strings.ToLower(os.Getenv("EMBED_BACKEND")) == "ollama" {
		return embed.NewOllamaEmbedder(embed.OllamaConfig{
			BaseURL: os.Getenv("OLLAMA_URL"),
			Model:   os.Getenv("OLLAMA_MODEL"),
		})
	}
	return embed.NewStubEmbedder(128)
}

func embedIndex(paths []string, watch bool) {
	if len(paths) == 0 {
		paths = []string{"."}
	}
	idx, err := openLocalIndex()
	if err != nil {
		fail("open index: %v", err)
		osExit(1)
	}
	defer idx.Close()

	w := embed.NewWatcher(idx, embed.WatchOptions{
		IncludeExts: []string{".go", ".md", ".markdown", ".txt"},
	})

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if watch {
		info("watching %s (Ctrl-C to stop)", strings.Join(paths, ", "))
		if err := w.Run(ctx, paths...); err != nil && err != context.Canceled {
			fail("watcher: %v", err)
			osExit(1)
		}
		s := idx.Stats()
		success("watch stopped: %d docs, %d chunks", s.Docs, s.Chunks)
		return
	}

	info("indexing %s", strings.Join(paths, ", "))
	t0 := time.Now()
	if err := w.ScanOnce(ctx, paths...); err != nil {
		fail("scan: %v", err)
		osExit(1)
	}
	if err := idx.Snapshot(); err != nil {
		fail("snapshot: %v", err)
		osExit(1)
	}
	s := idx.Stats()
	success("indexed %d docs, %d chunks in %s (snapshot=%s)",
		s.Docs, s.Chunks, time.Since(t0).Round(time.Millisecond), localSnapshotDir())
}

func embedQuery(args []string) {
	if len(args) == 0 {
		fail("usage: gofastr embed query \"<text>\" [-k N] [--hybrid] [--mmr 0.4]")
		osExit(1)
	}
	text := args[0]
	q := embed.Query{Text: text, K: 5}
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "-k", "--k":
			if i+1 >= len(args) {
				fail("missing value for %s", args[i])
				osExit(1)
			}
			n, err := strconv.Atoi(args[i+1])
			if err != nil {
				fail("invalid k: %v", err)
				osExit(1)
			}
			q.K = n
			i++
		case "--hybrid":
			q.Hybrid = true
		case "--mmr":
			if i+1 >= len(args) {
				fail("missing value for --mmr")
				osExit(1)
			}
			f, err := strconv.ParseFloat(args[i+1], 64)
			if err != nil {
				fail("invalid mmr: %v", err)
				osExit(1)
			}
			q.MMRLambda = f
			i++
		default:
			fail("unknown flag: %s", args[i])
			osExit(1)
		}
	}

	if url := os.Getenv("GOFASTR_URL"); url != "" {
		hits, err := remoteQuery(url, q)
		if err != nil {
			fail("remote query: %v", err)
			osExit(1)
		}
		printHits(hits)
		return
	}

	idx, err := openLocalIndex()
	if err != nil {
		fail("open index: %v", err)
		osExit(1)
	}
	defer idx.Close()
	hits, err := idx.Query(context.Background(), q)
	if err != nil {
		fail("query: %v", err)
		osExit(1)
	}
	printHits(hits)
}

func printHits(hits []embed.Hit) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(hits)
}

func embedStats() {
	if url := os.Getenv("GOFASTR_URL"); url != "" {
		body, err := remoteGet(url + "/embed/stats")
		if err != nil {
			fail("remote stats: %v", err)
			osExit(1)
		}
		fmt.Println(string(body))
		return
	}
	idx, err := openLocalIndex()
	if err != nil {
		fail("open index: %v", err)
		osExit(1)
	}
	defer idx.Close()
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(idx.Stats())
}

func embedClear() {
	dir := localSnapshotDir()
	if err := os.RemoveAll(dir); err != nil {
		fail("clear %s: %v", dir, err)
		osExit(1)
	}
	success("cleared %s", dir)
}

func remoteQuery(base string, q embed.Query) ([]embed.Hit, error) {
	body, err := json.Marshal(q)
	if err != nil {
		return nil, err
	}
	resp, err := http.Post(strings.TrimRight(base, "/")+"/embed/query", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		msg, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(msg))
	}
	var payload struct {
		Hits []embed.Hit `json:"hits"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	return payload.Hits, nil
}

func remoteGet(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		msg, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(msg))
	}
	return io.ReadAll(resp.Body)
}
