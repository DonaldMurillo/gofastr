package semanticcmd

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/semantic"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/internal/filelock"
	"github.com/DonaldMurillo/gofastr/internal/jsonexact"
)

// RunSemantic dispatches `gofastr semantic <subcommand>`.
//
//   - index <path...>     One-shot: walk the path(s) and add every
//     matching file to the local index.
//   - watch <path...>     Index, then poll for changes until SIGINT.
//   - query <text>        Print top-K hits as JSON.
//   - stats               Print index stats as JSON.
//   - clear               Delete the local snapshot + WAL for this cwd;
//     refused while an index or watch holds the writer lock.
//
// When the GOFASTR_URL environment variable is set, query and stats
// hit that server's /semantic/* endpoints instead of opening a local
// index. index/watch/clear are always local: long-lived mutations
// should run alongside the server, not through it.
func RunSemantic(args []string) {
	if len(args) == 0 || cli.HasHelpFlagBeforeTerminator(args) {
		printSemanticHelp()
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
		printSemanticHelp()
	default:
		cli.Fail("unknown semantic subcommand: %s", sub)
		printSemanticHelp()
		cli.OsExit(1)
	}
}

func printSemanticHelp() {
	fmt.Printf(`
%s: semantic search for the project at cwd

%s:
  gofastr semantic <subcommand> [args]

%s:
  index <path...>     One-shot index of the given paths
  watch <path...>     Index then re-scan on a 2s timer until SIGINT
  query "<text>"      Print top hits as JSON
  stats               Print index stats as JSON
  clear               Delete the local snapshot for this directory
                      (refused while an index or watch holds it)

%s:
  GOFASTR_URL=...     If set, query/stats hit the running server's
                      /semantic/* endpoints instead of opening a local
                      index. index/watch/clear are always local.
  GOFASTR_SEMANTIC_TOKEN=...  Bearer token for a server whose mount
                      sets WithAuthToken. Sent only to GOFASTR_URL;
                      a redirect is refused.
  SEMANTIC_BACKEND=ollama  Use the Ollama HTTP embedder instead of the
                      built-in stub. Set OLLAMA_URL and OLLAMA_MODEL
                      to override defaults (localhost:11434 +
                      nomic-embed-text). EMBED_BACKEND is read as a
                      deprecated fallback.

%s:
  gofastr semantic index .
  gofastr semantic watch ./src ./docs
  gofastr semantic query "authentication middleware"
`, cli.Bold("gofastr semantic"), cli.Bold("Usage"), cli.Bold("Subcommands"), cli.Bold("Environment"), cli.Bold("Examples"))
}

// localSnapshotDir returns ~/.gofastr/semantic/<cwd-hash>. Distinct cwds
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
	return filepath.Join(home, ".gofastr", "semantic", tag)
}

// openLocalIndex constructs an Index. The embedder is chosen by
// environment:
//
//   - SEMANTIC_BACKEND=ollama  → OllamaEmbedder (real semantic, requires
//     a running Ollama-compatible server)
//   - anything else            → StubEmbedder (deterministic, no setup,
//     low retrieval quality, fine for dev/tests)
//
// SEMANTIC_BACKEND is preferred; EMBED_BACKEND is read as a deprecated
// fallback and warns on stderr.
//
// Additional env knobs for the Ollama path:
//
//   - OLLAMA_URL    (default http://localhost:11434)
//   - OLLAMA_MODEL  (default nomic-embed-text)
//
// readOnly opens beside a running `semantic watch` (query, stats): the
// watcher holds the directory's writer lock, and a second writer
// snapshotted and truncated the WAL the watcher was appending to.
func openLocalIndex(readOnly bool) (semantic.Index, error) {
	idx, err := semantic.Open(semantic.Options{
		Embedder: chooseEmbedder(),
		Keyword:  semantic.NewMemoryKeyword(),
		Path:     localSnapshotDir(),
		ReadOnly: readOnly,
	})
	return idx, err
}

func chooseEmbedder() semantic.Embedder {
	backend := strings.ToLower(os.Getenv("SEMANTIC_BACKEND"))
	if backend == "" {
		if old := os.Getenv("EMBED_BACKEND"); old != "" {
			backend = strings.ToLower(old)
			fmt.Fprintln(os.Stderr, "warning: EMBED_BACKEND is deprecated; use SEMANTIC_BACKEND (using EMBED_BACKEND for this run)")
		}
	}
	if backend == "ollama" {
		return semantic.NewOllamaEmbedder(semantic.OllamaConfig{
			BaseURL: os.Getenv("OLLAMA_URL"),
			Model:   os.Getenv("OLLAMA_MODEL"),
		})
	}
	return semantic.NewStubEmbedder(128)
}

func embedIndex(paths []string, watch bool) {
	if len(paths) == 0 {
		paths = []string{"."}
	}
	idx, err := openLocalIndex(false)
	if err != nil {
		cli.Fail("open index: %v", err)
		cli.OsExit(1)
	}
	defer idx.Close()

	w := semantic.NewWatcher(idx, semantic.WatchOptions{
		IncludeExts: []string{".go", ".md", ".markdown", ".txt"},
	})

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if watch {
		cli.Info("watching %s (Ctrl-C to stop)", strings.Join(paths, ", "))
		if err := w.Run(ctx, paths...); err != nil && err != context.Canceled {
			cli.Fail("watcher: %v", err)
			cli.OsExit(1)
		}
		s := idx.Stats()
		cli.Success("watch stopped: %d docs, %d chunks", s.Docs, s.Chunks)
		return
	}

	cli.Info("indexing %s", strings.Join(paths, ", "))
	t0 := time.Now()
	if err := w.ScanOnce(ctx, paths...); err != nil {
		cli.Fail("scan: %v", err)
		cli.OsExit(1)
	}
	if err := idx.Snapshot(); err != nil {
		cli.Fail("snapshot: %v", err)
		cli.OsExit(1)
	}
	s := idx.Stats()
	cli.Success("indexed %d docs, %d chunks in %s (snapshot=%s)",
		s.Docs, s.Chunks, time.Since(t0).Round(time.Millisecond), localSnapshotDir())
}

const queryUsage = "usage: gofastr semantic query \"<text>\" [-k N] [--hybrid] [--mmr 0.4]"

// parseQueryArgs reads `semantic query` arguments: one query text and
// the -k/--k, --hybrid and --mmr flags, before or after it, as
// `-k 3` or `-k=3`. "--" ends the flags, for a query that starts with a
// dash. It refuses a k below one, an empty query and a second word, which
// an unquoted multi-word query is.
func parseQueryArgs(args []string) (semantic.Query, error) {
	q := semantic.Query{K: 5}
	var text []string
	flags := true
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !flags || arg == "" || !strings.HasPrefix(arg, "-") || arg == "-" {
			text = append(text, arg)
			continue
		}
		if arg == "--" {
			flags = false
			continue
		}
		name, value, hasValue := strings.Cut(arg, "=")
		switch name {
		case "-k", "--k", "--mmr":
			if !hasValue {
				if i+1 >= len(args) {
					return q, fmt.Errorf("missing value for %s", name)
				}
				i++
				value = args[i]
			}
			if name == "--mmr" {
				//gofastr:allow(nonfinite) CLI flag value is operator input; a non-finite --mmr fails downstream, not a request-borne bound check.
				f, err := strconv.ParseFloat(value, 64)
				if err != nil {
					return q, fmt.Errorf("invalid mmr: %v", err)
				}
				q.MMRLambda = f
				continue
			}
			n, err := strconv.Atoi(value)
			if err != nil || n < 1 {
				return q, fmt.Errorf("-k takes a positive number of hits, got %q", value)
			}
			q.K = n
		case "--hybrid":
			if hasValue {
				return q, errors.New("--hybrid takes no value")
			}
			q.Hybrid = true
		default:
			return q, fmt.Errorf("unknown flag: %s", arg)
		}
	}
	switch {
	case len(text) == 0:
		return q, errors.New(queryUsage)
	case len(text) > 1:
		return q, fmt.Errorf("semantic query takes one query, got %q and %q: quote a query of several words", text[0], text[1])
	case strings.TrimSpace(text[0]) == "":
		return q, errors.New("semantic query needs a non-empty query")
	}
	q.Text = text[0]
	return q, nil
}

func embedQuery(args []string) {
	q, err := parseQueryArgs(args)
	if err != nil {
		failSemanticJSON(err)
		cli.OsExit(1)
		return
	}

	if url := os.Getenv("GOFASTR_URL"); url != "" {
		hits, err := remoteQuery(url, q)
		if err != nil {
			failSemanticJSON(fmt.Errorf("remote query: %w", err))
			cli.OsExit(1)
			return
		}
		printHits(hits)
		return
	}

	idx, err := openLocalIndex(true)
	if err != nil {
		failSemanticJSON(fmt.Errorf("open index: %w", err))
		cli.OsExit(1)
		return
	}
	defer idx.Close()
	hits, err := idx.Query(context.Background(), q)
	if err != nil {
		failSemanticJSON(fmt.Errorf("query: %w", err))
		cli.OsExit(1)
		return
	}
	printHits(hits)
}

func printHits(hits []semantic.Hit) {
	if err := emitSemanticJSON(hits); err != nil {
		failSemanticJSON(fmt.Errorf("print hits: %w", err))
		cli.OsExit(1)
	}
}

func embedStats() {
	if url := os.Getenv("GOFASTR_URL"); url != "" {
		body, err := remoteGet(url + "/semantic/stats")
		if err == nil {
			err = cli.EmitRawJSON(body)
		}
		if err != nil {
			failSemanticJSON(fmt.Errorf("remote stats: %w", err))
			cli.OsExit(1)
			return
		}
		return
	}
	idx, err := openLocalIndex(true)
	if err != nil {
		failSemanticJSON(fmt.Errorf("open index: %w", err))
		cli.OsExit(1)
		return
	}
	defer idx.Close()
	if err := emitSemanticJSON(idx.Stats()); err != nil {
		failSemanticJSON(fmt.Errorf("print stats: %w", err))
		cli.OsExit(1)
	}
}

func emitSemanticJSON(document any) error {
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	return cli.EmitRawJSON(data)
}

func failSemanticJSON(err error) {
	defer cli.BeginJSON()()
	if emitErr := cli.EmitJSON(map[string]string{"error": err.Error()}); emitErr != nil {
		cli.Fail("write JSON: %v", emitErr)
	}
	cli.Fail("%v", err)
}

func embedClear() {
	dir := localSnapshotDir()
	if err := clearIndexDir(dir); err != nil {
		cli.Fail("clear %s: %v", dir, err)
		cli.OsExit(1)
	}
	cli.Success("cleared %s", dir)
}

// indexLockName is the writer lock battery/semantic keeps in an index
// directory (lockIndexDir in persist.go).
const indexLockName = "lock"

// clearIndexDir empties an index directory under its writer lock and
// keeps the lock file. Removing the whole directory unlinked the lock a
// running `semantic watch` held, so the next writer locked a new file
// and two processes wrote one WAL. A held lock refuses the clear; a
// writer that starts mid-clear finds the same file locked and fails with
// semantic.ErrIndexLocked instead of writing. A missing directory is
// already clear.
func clearIndexDir(dir string) error {
	root, err := os.OpenRoot(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer root.Close()
	lock, err := root.OpenFile(indexLockName, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("open index lock: %w", err)
	}
	defer lock.Close()
	held, err := filelock.TryLock(lock)
	if err != nil {
		return fmt.Errorf("lock index: %w", err)
	}
	if !held {
		return errors.New("another process has this index open for writing (a running `gofastr semantic watch`?); stop it, then clear")
	}
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.Name() == indexLockName {
			continue
		}
		if err := root.RemoveAll(e.Name()); err != nil {
			return err
		}
	}
	return nil
}

// remoteHTTPClient bounds every remote semantic call: a GOFASTR_URL peer
// that accepts the connection but never responds fails the call within
// the deadline instead of hanging the CLI (the bootProbeClient shape).
// It refuses redirects: a call carries GOFASTR_SEMANTIC_TOKEN, which goes
// to the server GOFASTR_URL names and nowhere a redirect points.
var remoteHTTPClient = &http.Client{
	Timeout: 2 * time.Second,
	CheckRedirect: func(req *http.Request, _ []*http.Request) error {
		return fmt.Errorf("refusing redirect to %s: GOFASTR_URL must name the server itself", req.URL.Redacted())
	},
}

// remoteDo sends req with the mount's bearer token when
// GOFASTR_SEMANTIC_TOKEN is set (the token the server passed to
// WithAuthToken).
func remoteDo(req *http.Request) (*http.Response, error) {
	if tok := os.Getenv("GOFASTR_SEMANTIC_TOKEN"); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	return remoteHTTPClient.Do(req)
}

// remoteMaxBodyBytes caps the bytes buffered from a remote semantic peer
// (repo convention, 1 MiB like the generated clients): a peer that
// dribbles a response body forever errors at the cap instead of pinning
// unbounded heap in the CLI process.
const remoteMaxBodyBytes = 1 << 20

func remoteQuery(base string, q semantic.Query) ([]semantic.Hit, error) {
	body, err := json.Marshal(q)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(base, "/")+"/semantic/query", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := remoteDo(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, remoteMaxBodyBytes))
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(msg))
	}
	var payload struct {
		Hits []semantic.Hit `json:"hits"`
	}
	dec := json.NewDecoder(io.LimitReader(resp.Body, remoteMaxBodyBytes))
	dec.UseNumber()
	if err := dec.Decode(&payload); err != nil {
		return nil, err
	}
	if err := jsonexact.Settle(&payload); err != nil {
		return nil, err
	}
	return payload.Hits, nil
}

func remoteGet(url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := remoteDo(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, remoteMaxBodyBytes))
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(msg))
	}
	return io.ReadAll(io.LimitReader(resp.Body, remoteMaxBodyBytes))
}
