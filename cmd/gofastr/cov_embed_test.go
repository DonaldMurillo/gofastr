package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/embed"
)

// covT_embedSandbox isolates HOME (snapshot dir) and cwd so embed tests
// neither read the real index nor reach any external server.
func covT_embedSandbox(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	// os.UserHomeDir reads USERPROFILE on Windows; HOME alone only isolates
	// Unix. Keep both pointed at the test sandbox so snapshot/WAL files never
	// touch the developer's real index (or collide with another process).
	t.Setenv("USERPROFILE", home)
	if got, err := os.UserHomeDir(); err != nil || filepath.Clean(got) != filepath.Clean(home) {
		t.Fatalf("embed sandbox did not isolate user home: got=%q err=%v want=%q", got, err, home)
	}
	t.Setenv("GOFASTR_URL", "")
	t.Setenv("EMBED_BACKEND", "")
	work := t.TempDir()
	covT_chdir(t, work)
	if err := os.WriteFile(filepath.Join(work, "doc.md"), []byte("# Title\nauthentication middleware here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return work
}

func TestRunEmbedHelpAndUnknown(t *testing.T) {
	covT_embedSandbox(t)
	out := covT_capStdout(t, func() { runEmbed(nil) })
	if !strings.Contains(out, "semantic search") {
		t.Fatalf("help: %s", out)
	}
	out = covT_capStdout(t, func() { runEmbed([]string{"help"}) })
	if !strings.Contains(out, "Subcommands") {
		t.Fatalf("help sub: %s", out)
	}
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { runEmbed([]string{"bogus"}) })
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestEmbedIndexQueryStatsClear(t *testing.T) {
	covT_embedSandbox(t)
	var indexOut string
	code := covT_capExit(t, func() {
		indexOut = covT_capStdout(t, func() { runEmbed([]string{"index", "."}) })
	})
	if code != -1 {
		t.Fatalf("embed index exited %d: %s", code, indexOut)
	}
	out := covT_capStdout(t, func() { runEmbed([]string{"query", "authentication", "-k", "3", "--hybrid"}) })
	if !strings.HasPrefix(strings.TrimSpace(out), "[") {
		t.Fatalf("query should print JSON array: %s", out)
	}
	covT_capStdout(t, func() { runEmbed([]string{"stats"}) })
	covT_capStdout(t, func() { runEmbed([]string{"clear"}) })
}

func TestEmbedQueryNoArgsExits(t *testing.T) {
	covT_embedSandbox(t)
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { embedQuery(nil) })
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestEmbedQueryFlagParsing(t *testing.T) {
	covT_embedSandbox(t)
	covT_capStdout(t, func() { runEmbed([]string{"index", "."}) })
	// --mmr value
	covT_capStdout(t, func() { embedQuery([]string{"auth", "--mmr", "0.4"}) })
	// bad k
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { embedQuery([]string{"auth", "-k", "notnum"}) })
	})
	if code != 1 {
		t.Fatalf("bad k want 1 got %d", code)
	}
	// missing k value
	code = covT_capExit(t, func() {
		covT_capStdout(t, func() { embedQuery([]string{"auth", "-k"}) })
	})
	if code != 1 {
		t.Fatalf("missing k want 1 got %d", code)
	}
	// missing mmr value
	code = covT_capExit(t, func() {
		covT_capStdout(t, func() { embedQuery([]string{"auth", "--mmr"}) })
	})
	if code != 1 {
		t.Fatalf("missing mmr want 1 got %d", code)
	}
	// bad mmr
	code = covT_capExit(t, func() {
		covT_capStdout(t, func() { embedQuery([]string{"auth", "--mmr", "nope"}) })
	})
	if code != 1 {
		t.Fatalf("bad mmr want 1 got %d", code)
	}
	// unknown flag
	code = covT_capExit(t, func() {
		covT_capStdout(t, func() { embedQuery([]string{"auth", "--zzz"}) })
	})
	if code != 1 {
		t.Fatalf("unknown flag want 1 got %d", code)
	}
}

func TestChooseEmbedderOllama(t *testing.T) {
	t.Setenv("EMBED_BACKEND", "ollama")
	if _, ok := chooseEmbedder().(*embed.OllamaEmbedder); !ok {
		t.Fatal("expected OllamaEmbedder when EMBED_BACKEND=ollama")
	}
	t.Setenv("EMBED_BACKEND", "")
	if chooseEmbedder() == nil {
		t.Fatal("stub embedder nil")
	}
}

func TestLocalSnapshotDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := localSnapshotDir()
	if !strings.Contains(dir, ".gofastr") || !strings.Contains(dir, "embed") {
		t.Fatalf("snapshot dir = %q", dir)
	}
}

func TestEmbedRemoteQueryAndStats(t *testing.T) {
	covT_embedSandbox(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/embed/query":
			w.Write([]byte(`{"hits":[{"chunk":{"doc_id":"a.go","text":"x"},"score":0.9}]}`))
		case "/embed/stats":
			w.Write([]byte(`{"docs":1,"chunks":2}`))
		default:
			http.Error(w, "no", http.StatusNotFound)
		}
	}))
	defer srv.Close()
	t.Setenv("GOFASTR_URL", srv.URL)

	out := covT_capStdout(t, func() { runEmbed([]string{"query", "x"}) })
	if !strings.Contains(out, "a.go") {
		t.Fatalf("remote query: %s", out)
	}
	out = covT_capStdout(t, func() { runEmbed([]string{"stats"}) })
	if !strings.Contains(out, "docs") {
		t.Fatalf("remote stats: %s", out)
	}
}

func TestRemoteQueryHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad", http.StatusInternalServerError)
	}))
	defer srv.Close()
	if _, err := remoteQuery(srv.URL, embed.Query{Text: "x"}); err == nil {
		t.Fatal("expected HTTP error from remoteQuery")
	}
	if _, err := remoteGet(srv.URL + "/embed/stats"); err == nil {
		t.Fatal("expected HTTP error from remoteGet")
	}
}

func TestPrintHits(t *testing.T) {
	out := covT_capStdout(t, func() {
		printHits([]embed.Hit{{Chunk: embed.Chunk{DocID: "x.go", Text: "hi"}, Score: 0.5}})
	})
	if !strings.Contains(out, "x.go") {
		t.Fatalf("printHits: %s", out)
	}
}
