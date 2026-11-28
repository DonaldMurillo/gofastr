package docscmd

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/isolation"
)

func TestDocsSiteRefreshReplacesTheCachedCopy(t *testing.T) {
	cache := t.TempDir()
	stubRelease(t, "v1.2.3", buildSiteTarball(t, []tarEntry{{name: "index.html", body: "old"}}), "")
	if _, err := ensureDocsSite(context.Background(), cache, "v1.2.3", false); err != nil {
		t.Fatal(err)
	}
	stubRelease(t, "v1.2.3", buildSiteTarball(t, []tarEntry{{name: "index.html", body: "new"}}), "")
	dir, err := ensureDocsSite(context.Background(), cache, "v1.2.3", true)
	if err != nil {
		t.Fatal(err)
	}
	if body, _ := os.ReadFile(filepath.Join(dir, "index.html")); string(body) != "new" {
		t.Fatalf("--refresh kept %q, want the new copy", body)
	}
}

func TestSwapIntoPlaceKeepsAConcurrentCopy(t *testing.T) {
	cache := t.TempDir()
	root, err := openCacheRoot(cache)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	for _, d := range []string{"v1/x", ".tmp/x"} {
		if err := root.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := swapIntoPlace(root, ".tmp", "v1", false); err != nil {
		t.Fatalf("a copy that appeared meanwhile is as good: %v", err)
	}
	if _, err := root.Stat(".tmp"); err == nil {
		t.Fatal("the losing temp copy was left behind")
	}
}

func TestDocsSiteArchiveCaps(t *testing.T) {
	if docsSiteMaxEntries != 50_000 || docsSiteMaxExtract != 512<<20 {
		t.Fatalf("caps = %d entries, %d bytes; the rule says 50,000 entries and 512 MiB", docsSiteMaxEntries, docsSiteMaxExtract)
	}
	many := make([]tarEntry, docsSiteMaxEntries+1)
	for i := range many {
		many[i] = tarEntry{name: "./", typeflag: tar.TypeDir}
	}
	var huge bytes.Buffer
	gz := gzip.NewWriter(&huge)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "index.html", Typeflag: tar.TypeReg, Size: docsSiteMaxExtract + 1, Mode: 0o644})
	_ = tw.Flush()
	_ = gz.Close()
	for name, tc := range map[string]struct {
		archive []byte
		want    string
	}{
		"entries": {buildSiteTarball(t, many), "more than"},
		"size":    {huge.Bytes(), "exceeds"},
	} {
		t.Run(name, func(t *testing.T) {
			cache := t.TempDir()
			root, err := openCacheRoot(cache)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			if err := root.Mkdir("x", 0o700); err != nil {
				t.Fatal(err)
			}
			if err := extractDocsSite(tc.archive, root, "x"); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("extract = %v, want an error naming the cap (%q)", err, tc.want)
			}
		})
	}
}

func TestDocsSiteRefusesAnInnerParentSegment(t *testing.T) {
	cache := t.TempDir()
	stubRelease(t, "v1.2.3", buildSiteTarball(t, []tarEntry{{name: "index.html", body: "x"}, {name: "docs/../other.html", body: "x"}}), "")
	if _, err := ensureDocsSite(context.Background(), cache, "v1.2.3", false); err == nil {
		t.Fatal("an entry with a parent segment was extracted")
	}
}

func TestDocsSiteCacheIsOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX modes")
	}
	cache := filepath.Join(t.TempDir(), "cache")
	root, err := openCacheRoot(cache)
	if err != nil {
		t.Fatal(err)
	}
	_ = root.Close()
	if st, err := os.Stat(cache); err != nil || st.Mode().Perm() != 0o700 {
		t.Fatalf("cache dir mode = %v, %v; want 0700", st.Mode().Perm(), err)
	}
}

func TestFetchCappedRefusesALargerBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "0123456789")
	}))
	defer srv.Close()
	if _, err := fetchCapped(context.Background(), srv.Client(), srv.URL, 9); err == nil || !strings.Contains(err.Error(), "larger than 9") {
		t.Fatalf("fetchCapped over the cap = %v", err)
	}
	if body, err := fetchCapped(context.Background(), srv.Client(), srv.URL, 10); err != nil || string(body) != "0123456789" {
		t.Fatalf("fetchCapped at the cap = %q, %v", body, err)
	}
}

func TestDocsServeFullRefusesAnInstallWithoutTheBinary(t *testing.T) {
	prev := goInstallSite
	goInstallSite = func(context.Context, string, string) error { return nil }
	t.Cleanup(func() { goInstallSite = prev })
	cache := t.TempDir()
	root, err := openCacheRoot(cache)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := installFullDocsSite(context.Background(), root, cache, "v1.2.3", "site", false); err == nil || !strings.Contains(err.Error(), "produced no") {
		t.Fatalf("install with no binary = %v", err)
	}
	if _, err := root.Stat("full/v1.2.3"); err == nil {
		t.Fatal("an empty install was put in place")
	}
}

func TestDocsListenAddrIsLoopback(t *testing.T) {
	if got := loopbackAddr(8083); got != "127.0.0.1:8083" {
		t.Fatalf("loopbackAddr = %q", got)
	}
}

// The static server is started under forced worktree isolation, so it must
// both turn isolation off and bind 127.0.0.1 at the port asked for.
func TestStaticDocsServerListensOnLoopbackAtPort(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "gofastr.settings.yml"), []byte("isolation:\n  mode: always\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(project)
	t.Setenv(isolation.EnvIsolation, "")
	export := t.TempDir()
	if err := os.WriteFile(filepath.Join(export, "index.html"), []byte("<h1>home</h1>"), 0o600); err != nil {
		t.Fatal(err)
	}
	for attempt := 1; ; attempt++ {
		err := startStaticDocsSiteOnce(t, export, freePort(t))
		if err == nil {
			return
		}
		if errors.Is(err, syscall.EADDRINUSE) && attempt < 5 {
			continue
		}
		t.Fatalf("static docs server exited before listening: %v", err)
	}
}

// startStaticDocsSiteOnce returns the error startDocsSite gave before it
// listened, and fails the test on any other wrong outcome.
func startStaticDocsSiteOnce(t *testing.T, export string, port int) error {
	t.Helper()
	app, err := newDocsSiteApp(export)
	if err != nil {
		t.Fatal(err)
	}
	bound := make(chan string, 1)
	app.OnReady(func(addr string) {
		bound <- addr
		go func() { _ = app.Shutdown(context.Background()) }()
	})
	done := make(chan error, 1)
	go func() { done <- startDocsSite(app, docsServeOptions{port: port}) }()
	var addr string
	select {
	case addr = <-bound:
	case err := <-done:
		select {
		case addr = <-bound:
			done <- err
		default:
			if err == nil {
				t.Fatal("static docs server returned without listening")
			}
			return err
		}
	case <-time.After(30 * time.Second):
		t.Fatal("static docs server never listened")
	}
	if want := net.JoinHostPort("127.0.0.1", strconv.Itoa(port)); addr != want {
		t.Fatalf("static docs server listened on %s, want %s", addr, want)
	}
	if err := <-done; err != nil {
		t.Fatalf("startDocsSite = %v", err)
	}
	return nil
}
