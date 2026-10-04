package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

type tarEntry struct {
	name     string
	body     string
	typeflag byte
	linkname string
}

func buildSiteTarball(t *testing.T, entries []tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		hdr := &tar.Header{Name: e.name, Mode: 0o644, Typeflag: e.typeflag, Linkname: e.linkname}
		if hdr.Typeflag == 0 {
			hdr.Typeflag = tar.TypeReg
		}
		if hdr.Typeflag == tar.TypeReg {
			hdr.Size = int64(len(e.body))
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if hdr.Typeflag == tar.TypeReg {
			if _, err := io.WriteString(tw, e.body); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// stubRelease serves the archive and its checksum the way a GitHub release
// does, and points the CLI at it. sum overrides the published checksum.
func stubRelease(t *testing.T, tag string, archive []byte, sum string) *atomic.Int32 {
	t.Helper()
	if sum == "" {
		h := sha256.Sum256(archive)
		sum = hex.EncodeToString(h[:])
	}
	var hits atomic.Int32
	asset := "/" + tag + "/gofastr-site-" + tag + ".tar.gz"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch r.URL.Path {
		case asset:
			_, _ = w.Write(archive)
		case asset + ".sha256":
			_, _ = io.WriteString(w, sum+"  gofastr-site-"+tag+".tar.gz\n")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	prev := docsSiteReleaseURL
	docsSiteReleaseURL = srv.URL
	t.Cleanup(func() { docsSiteReleaseURL = prev })
	return &hits
}

var siteEntries = []tarEntry{
	{name: "./", typeflag: tar.TypeDir},
	{name: "./index.html", body: "<h1>home</h1>"},
	{name: "./docs/cli/index.html", body: "<h1>cli</h1>"},
	{name: "./__gofastr/runtime.js", body: "// rt"},
}

func TestDocsSiteDownloadsOnceThenCaches(t *testing.T) {
	cache := t.TempDir()
	hits := stubRelease(t, "v1.2.3", buildSiteTarball(t, siteEntries), "")

	dir, err := ensureDocsSite(context.Background(), cache, "v1.2.3", false)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "docs", "cli", "index.html"))
	if err != nil || string(got) != "<h1>cli</h1>" {
		t.Fatalf("docs/cli/index.html = %q, %v", got, err)
	}
	first := hits.Load()

	if _, err := ensureDocsSite(context.Background(), cache, "v1.2.3", false); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != first {
		t.Fatalf("cached run fetched again: %d requests, want %d", hits.Load(), first)
	}
	if _, err := ensureDocsSite(context.Background(), cache, "v1.2.3", true); err != nil {
		t.Fatal(err)
	}
	if hits.Load() == first {
		t.Fatal("--refresh did not download again")
	}
}

func TestDocsSiteChecksumMismatchRefused(t *testing.T) {
	cache := t.TempDir()
	stubRelease(t, "v1.2.3", buildSiteTarball(t, siteEntries), strings.Repeat("ab", 32))

	_, err := ensureDocsSite(context.Background(), cache, "v1.2.3", false)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("err = %v, want checksum mismatch", err)
	}
	assertNoCachedSite(t, cache, "v1.2.3")
}

func TestDocsSiteMissingAssetNamesFull(t *testing.T) {
	cache := t.TempDir()
	stubRelease(t, "v1.2.3", nil, "")

	_, err := ensureDocsSite(context.Background(), cache, "v0.90.0", false)
	if err == nil || !strings.Contains(err.Error(), "--full") {
		t.Fatalf("err = %v, want a pointer to --full", err)
	}
	// Below the --full floor the hint must not send the user to a mode
	// that refuses the tag.
	_, err = ensureDocsSite(context.Background(), cache, "v0.81.0", false)
	if err == nil || strings.Contains(err.Error(), "--full") || !strings.Contains(err.Error(), "gofastr docs") {
		t.Fatalf("err = %v, want the markdown pointer and no --full", err)
	}
}

func TestDocsSiteRefusesUnsafeEntries(t *testing.T) {
	cases := map[string][]tarEntry{
		"parent segment": {{name: "index.html", body: "x"}, {name: "../escape.html", body: "x"}},
		"nested parent":  {{name: "index.html", body: "x"}, {name: "docs/../../escape.html", body: "x"}},
		"absolute":       {{name: "index.html", body: "x"}, {name: "/tmp/escape.html", body: "x"}},
		"backslash":      {{name: "index.html", body: "x"}, {name: `..\escape.html`, body: "x"}},
		"symlink":        {{name: "index.html", body: "x"}, {name: "link", typeflag: tar.TypeSymlink, linkname: "/etc/passwd"}},
		"hardlink":       {{name: "index.html", body: "x"}, {name: "link", typeflag: tar.TypeLink, linkname: "index.html"}},
		"duplicate":      {{name: "index.html", body: "x"}, {name: "index.html", body: "y"}},
		"no index":       {{name: "docs/index.html", body: "x"}},
	}
	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			cache := t.TempDir()
			stubRelease(t, "v1.2.3", buildSiteTarball(t, entries), "")
			if _, err := ensureDocsSite(context.Background(), cache, "v1.2.3", false); err == nil {
				t.Fatal("unsafe archive extracted")
			}
			assertNoCachedSite(t, cache, "v1.2.3")
			// Extraction runs in cache/.<tag>-<nonce>/, so one parent
			// segment lands in cache and two land beside it.
			for _, dir := range []string{cache, filepath.Dir(cache)} {
				if _, err := os.Stat(filepath.Join(dir, "escape.html")); err == nil {
					t.Fatalf("entry escaped the extraction root into %s", dir)
				}
			}
		})
	}
}

func TestDocsSiteFilesOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX modes")
	}
	cache := t.TempDir()
	stubRelease(t, "v1.2.3", buildSiteTarball(t, siteEntries), "")
	dir, err := ensureDocsSite(context.Background(), cache, "v1.2.3", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"index.html", "docs"} {
		st, err := os.Stat(filepath.Join(dir, p))
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm()&0o077 != 0 {
			t.Fatalf("%s mode %v, want no group/other bits", p, st.Mode().Perm())
		}
	}
}

// assertNoCachedSite checks a failed download left neither the final
// directory nor a temp extraction behind.
func assertNoCachedSite(t *testing.T, cache, tag string) {
	t.Helper()
	ents, err := os.ReadDir(cache)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		if strings.Contains(e.Name(), tag) {
			t.Fatalf("failed download left %s in the cache", e.Name())
		}
	}
}

func TestDocsSiteTagResolution(t *testing.T) {
	if got, err := docsSiteTag("0.86.0"); err != nil || got != "v0.86.0" {
		t.Fatalf("docsSiteTag(0.86.0) = %q, %v", got, err)
	}
	for _, bad := range []string{"../v1.0.0", "v1", "latest", "v1.0.0/../../x"} {
		if _, err := docsSiteTag(bad); err == nil {
			t.Fatalf("docsSiteTag(%q) accepted", bad)
		}
	}
	if _, err := docsSiteTag("v0.86.1-0.20261004153330-8e9c2aee62f5"); err == nil {
		t.Fatal("--release accepted a pseudo-version")
	}

	prev := docsSiteInstalledVersion
	t.Cleanup(func() { docsSiteInstalledVersion = prev })
	docsSiteInstalledVersion = func() string { return "v0.86.0" }
	if got, err := docsSiteTag(""); err != nil || got != "v0.86.0" {
		t.Fatalf("installed release: %q, %v", got, err)
	}
	for _, built := range []string{
		"",
		"v0.86.1-0.20261004153330-8e9c2aee62f5",
		"v0.86.1-0.20261004153330-8e9c2aee62f5+dirty",
		"v0.86.0+dirty",
	} {
		docsSiteInstalledVersion = func() string { return built }
		if _, err := docsSiteTag(""); err == nil || !strings.Contains(err.Error(), "--release") {
			t.Fatalf("installed %q: err = %v, want a pointer to --release", built, err)
		}
	}
}

func TestDocsSiteAppServesExport(t *testing.T) {
	dir := t.TempDir()
	for p, body := range map[string]string{
		"index.html":           "<h1>home</h1>",
		"docs/cli/index.html":  "<h1>cli</h1>",
		"__gofastr/runtime.js": "// rt",
		"404.html":             "<h1>lost</h1>",
	} {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	app, err := newDocsSiteApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{
		"/":                     "<h1>home</h1>",
		"/docs/cli/":            "<h1>cli</h1>",
		"/__gofastr/runtime.js": "// rt",
	} {
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK || rec.Body.String() != want {
			t.Fatalf("GET %s = %d %q, want 200 %q", path, rec.Code, rec.Body.String(), want)
		}
	}
	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/no/such/page/", nil))
	if rec.Code != http.StatusNotFound || rec.Body.String() != "<h1>lost</h1>" {
		t.Fatalf("miss = %d %q, want 404 with the export's 404.html", rec.Code, rec.Body.String())
	}
	if _, err := newDocsSiteApp(t.TempDir()); err == nil {
		t.Fatal("a directory with no index.html was accepted as an export")
	}
}

// stubGoInstall replaces go install with a shell-script "site" that
// records its environment to the returned path and exits.
func stubGoInstall(t *testing.T) (envOut string, installs *atomic.Int32) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("stub site binary is a shell script")
	}
	envOut = filepath.Join(t.TempDir(), "env")
	installs = new(atomic.Int32)
	prev := goInstallSite
	goInstallSite = func(_ context.Context, tag, gobin string) error {
		installs.Add(1)
		script := "#!/bin/sh\nenv > '" + envOut + "'\nexit 0\n"
		return os.WriteFile(filepath.Join(gobin, "site"), []byte(script), 0o700)
	}
	t.Cleanup(func() { goInstallSite = prev })
	return envOut, installs
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().(*net.TCPAddr).Port
}

func TestDocsServeFullInstallsOnce(t *testing.T) {
	envOut, installs := stubGoInstall(t)
	cache := t.TempDir()
	port := freePort(t)
	opts := docsServeOptions{port: port}
	for range 2 {
		if err := runFullDocsSite(context.Background(), cache, "v1.2.3", opts); err != nil {
			t.Fatal(err)
		}
	}
	if installs.Load() != 1 {
		t.Fatalf("go install ran %d times, want 1", installs.Load())
	}
	env, err := os.ReadFile(envOut)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"PORT=127.0.0.1:" + strconv.Itoa(port) + "\n", "GOFASTR_ISOLATION=off\n"} {
		if !strings.Contains(string(env), want) {
			t.Errorf("site env lacks %q", strings.TrimSpace(want))
		}
	}

	opts.refresh = true
	if err := runFullDocsSite(context.Background(), cache, "v1.2.3", opts); err != nil {
		t.Fatal(err)
	}
	if installs.Load() != 2 {
		t.Fatalf("--refresh --full ran go install %d times in total, want 2", installs.Load())
	}
	assertNoCachedSite(t, filepath.Join(cache, "full"), ".v1.2.3")
}

func TestDocsServeFullRefusesBusyPort(t *testing.T) {
	envOut, installs := stubGoInstall(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	port := ln.Addr().(*net.TCPAddr).Port

	err = runFullDocsSite(context.Background(), t.TempDir(), "v1.2.3", docsServeOptions{port: port})
	if err == nil || !strings.Contains(err.Error(), "in use") {
		t.Fatalf("err = %v, want port in use", err)
	}
	if _, err := os.Stat(envOut); err == nil {
		t.Fatal("the site started although the port was taken")
	}
	if installs.Load() != 0 {
		t.Fatal("go install ran before the busy port was reported")
	}
}

func TestDocsServeFullRefusesOldRelease(t *testing.T) {
	_, installs := stubGoInstall(t)
	prev := docsSiteCacheRoot
	cache := t.TempDir()
	docsSiteCacheRoot = func() (string, error) { return cache, nil }
	t.Cleanup(func() { docsSiteCacheRoot = prev })
	opts := docsServeOptions{port: freePort(t), full: true, version: "v0.81.9"}
	err := docsServe(context.Background(), opts)
	if err == nil || !strings.Contains(err.Error(), docsFullMinRelease) {
		t.Fatalf("err = %v, want the %s floor named", err, docsFullMinRelease)
	}
	if installs.Load() != 0 {
		t.Fatal("go install ran for a release below the floor")
	}
}

func TestDocsServeFullNeedsFixedPort(t *testing.T) {
	prev := docsSiteCacheRoot
	cache := t.TempDir()
	docsSiteCacheRoot = func() (string, error) { return cache, nil }
	t.Cleanup(func() { docsSiteCacheRoot = prev })
	err := docsServe(context.Background(), docsServeOptions{port: 0, full: true, version: "v1.2.3"})
	if err == nil || !strings.Contains(err.Error(), "--port") {
		t.Fatalf("err = %v, want a fixed --port demanded", err)
	}
}

func TestDocsSiteFailedRefreshKeepsCache(t *testing.T) {
	cache := t.TempDir()
	stubRelease(t, "v1.2.3", buildSiteTarball(t, siteEntries), "")
	dir, err := ensureDocsSite(context.Background(), cache, "v1.2.3", false)
	if err != nil {
		t.Fatal(err)
	}
	stubRelease(t, "v1.2.3", buildSiteTarball(t, siteEntries), strings.Repeat("ab", 32))
	if _, err := ensureDocsSite(context.Background(), cache, "v1.2.3", true); err == nil {
		t.Fatal("refresh with a bad checksum succeeded")
	}
	if _, err := os.Stat(filepath.Join(dir, "index.html")); err != nil {
		t.Fatalf("a failed --refresh discarded the cached copy: %v", err)
	}
}

func TestDocsServeDirRejectsDownloadFlags(t *testing.T) {
	err := docsServe(context.Background(), docsServeOptions{port: 8083, dir: t.TempDir(), full: true})
	if err == nil || !strings.Contains(err.Error(), "--dir") {
		t.Fatalf("err = %v", err)
	}
	if err := docsServe(context.Background(), docsServeOptions{port: 70000}); err == nil {
		t.Fatal("out-of-range port accepted")
	}
}

func TestReleaseAttachesDocsSite(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	rel := string(body)
	asset := docsSiteAsset("${TAG}")
	for _, want := range []string{
		"--export /tmp/docs-site",
		`sha256sum "$asset" > "$asset.sha256"`,
		`"/tmp/` + asset + `" "/tmp/` + asset + `.sha256"`,
	} {
		if !strings.Contains(rel, want) {
			t.Errorf("release.yml lacks %q: docs serve would download nothing", want)
		}
	}
	if !strings.Contains(rel, `asset="`+asset+`"`) {
		t.Errorf("release.yml builds an asset not named %s", asset)
	}
	// The build takes minutes; it must finish before the re-verify that
	// closes the tag-still-main-head window, never inside it.
	gate := strings.Index(rel, "name: Run the publish gate")
	build := strings.Index(rel, "name: Build the docs-site archive")
	reverify := strings.Index(rel, "name: Re-verify the tag is still main head")
	if gate < 0 || build < 0 || reverify < 0 || !(gate < build && build < reverify) {
		t.Errorf("release.yml step order gate=%d build=%d reverify=%d; want gate < build < reverify", gate, build, reverify)
	}
}
