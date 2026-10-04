package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/DonaldMurillo/gofastr/core/static"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/internal/upgrade"
	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
)

// docsSiteReleaseURL is where release.yml attaches the static export of
// examples/site. A var so tests point it at an httptest server.
var docsSiteReleaseURL = "https://github.com/DonaldMurillo/gofastr/releases/download"

// docsSiteCacheRoot returns the directory holding one extracted export per
// release tag. A var so tests redirect it into t.TempDir.
var docsSiteCacheRoot = func() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "gofastr", "docs-site"), nil
}

// goInstallSite builds examples/site at tag into gobin. A var so tests
// stub the toolchain out.
var goInstallSite = func(ctx context.Context, tag, gobin string) error {
	cmd := exec.CommandContext(ctx, "go", "install",
		"-ldflags=-X=main.siteVersion="+strings.TrimPrefix(tag, "v"),
		gofastrModule+"/examples/site@"+tag)
	cmd.Env = append(os.Environ(), "GOBIN="+gobin)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	cmd.WaitDelay = 5 * time.Second
	return cmd.Run()
}

// Download and extraction caps. The v0.86.0 export is 6 MB compressed and
// 32 MB on disk across ~1,000 files; the caps leave an order of magnitude
// of headroom and stop a hostile or corrupt archive from filling the disk.
const (
	docsSiteMaxArchive  = 128 << 20
	docsSiteMaxExtract  = 512 << 20
	docsSiteMaxEntries  = 50_000
	docsSiteHTTPTimeout = 5 * time.Minute
)

type docsServeOptions struct {
	port    int
	version string
	dir     string
	full    bool
	refresh bool
	open    bool
}

// runDocsServe implements `gofastr docs serve`: the docs site
// (examples/site) on localhost, for offline reading.
func runDocsServe(args []string) {
	flags := flag.NewFlagSet("docs serve", flag.ContinueOnError)
	var opts docsServeOptions
	flags.IntVar(&opts.port, "port", 8083, "port to listen on (127.0.0.1)")
	flags.StringVar(&opts.version, "release", "", "release tag to serve (default: this binary's version)")
	flags.StringVar(&opts.dir, "dir", "", "serve an existing static export instead of downloading one")
	flags.BoolVar(&opts.full, "full", false, "build and run the live site with `go install` (every demo works)")
	flags.BoolVar(&opts.refresh, "refresh", false, "discard the cached copy and download it again")
	flags.BoolVar(&opts.open, "open", false, "open the site in the default browser")
	flags.Usage = printDocsServeHelp
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		osExit(2)
		return
	}
	if flags.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "unexpected argument %q\n\n", flags.Arg(0))
		printDocsServeHelp()
		osExit(2)
		return
	}
	if err := docsServe(context.Background(), opts); err != nil {
		fail("%v", err)
		osExit(1)
	}
}

func printDocsServeHelp() {
	fmt.Fprint(os.Stderr, `gofastr docs serve: browse the docs site offline

Usage:
  gofastr docs serve [flags]

Downloads the static export of the docs site for this binary's release
once, caches it, and serves it on 127.0.0.1. Later runs need no network.

Flags:
  --port N           port to listen on (default 8083)
  --open             open the site in the default browser
  --release vX.Y.Z   serve another release's docs
  --refresh          download (or with --full, rebuild) the cached copy
  --dir PATH         serve an existing export (site --export PATH)
  --full             build the live site with go install and run it, so
                     the server-backed demos work too (needs network and
                     a Go toolchain on first run)
`)
}

func docsServe(ctx context.Context, opts docsServeOptions) error {
	if opts.port < 0 || opts.port > 65535 {
		return fmt.Errorf("--port %d is out of range", opts.port)
	}
	if opts.dir != "" {
		if opts.full || opts.version != "" || opts.refresh {
			return errors.New("--dir serves a local export; it does not combine with --full, --release or --refresh")
		}
		return serveDocsDir(opts.dir, opts)
	}
	tag, err := docsSiteTag(opts.version)
	if err != nil {
		return err
	}
	cacheRoot, err := docsSiteCacheRoot()
	if err != nil {
		return fmt.Errorf("locate cache directory: %w", err)
	}
	if opts.full {
		if opts.port == 0 {
			return errors.New("--full needs a fixed --port: the CLI waits on it to know the site is up")
		}
		if semver.Compare(tag, docsFullMinRelease) < 0 {
			return fmt.Errorf("--full needs %s or later: the site in %s cannot bind 127.0.0.1", docsFullMinRelease, tag)
		}
		return runFullDocsSite(ctx, cacheRoot, tag, opts)
	}
	dir, err := ensureDocsSite(ctx, cacheRoot, tag, opts.refresh)
	if err != nil {
		return err
	}
	return serveDocsDir(dir, opts)
}

// docsSiteTag picks the release whose docs to serve: the flag, else the
// version this binary was installed at. A dev build has no release, so it
// must name one.
func docsSiteTag(flagVersion string) (string, error) {
	if flagVersion != "" {
		v := flagVersion
		if !strings.HasPrefix(v, "v") {
			v = "v" + v
		}
		if err := upgrade.ValidateSemver(v); err != nil {
			return "", fmt.Errorf("--release: %w", err)
		}
		if !isReleaseTag(v) {
			return "", fmt.Errorf("--release %s: not a release tag", v)
		}
		return v, nil
	}
	if v := docsSiteInstalledVersion(); isReleaseTag(v) {
		return v, nil
	}
	return "", errors.New("this gofastr binary was not installed from a release tag (a local or @main build); " +
		"pass --release vX.Y.Z, or --dir with a local export (go run ./examples/site --export <dir>)")
}

// docsSiteInstalledVersion is the version this binary was installed at. A
// var so tests can stand in a release, pseudo or dirty build.
var docsSiteInstalledVersion = installedFrameworkVersion

// isReleaseTag reports whether v names a tag that can carry a release
// archive: a pseudo-version (an untagged commit, `go install ...@main`) or
// a version with build metadata (a `+dirty` local build) cannot.
func isReleaseTag(v string) bool {
	return v != "" && !strings.Contains(v, "+") && !module.IsPseudoVersion(v)
}

// ensureDocsSite returns the extracted export for tag, downloading it on
// a cache miss. The archive extracts into a temp sibling and is renamed
// into place, so a present directory is always a complete one.
func ensureDocsSite(ctx context.Context, cacheRoot, tag string, refresh bool) (string, error) {
	root, err := openCacheRoot(cacheRoot)
	if err != nil {
		return "", err
	}
	defer func() { _ = root.Close() }()
	dest := filepath.Join(cacheRoot, tag)
	if st, err := root.Stat(tag); !refresh && err == nil && st.IsDir() {
		return dest, nil
	}

	asset := docsSiteAsset(tag)
	base := strings.TrimSuffix(docsSiteReleaseURL, "/") + "/" + tag + "/" + asset
	info("Downloading the %s docs site (one time)…", tag)

	ctx, cancel := context.WithTimeout(ctx, docsSiteHTTPTimeout)
	defer cancel()
	client := &http.Client{Timeout: docsSiteHTTPTimeout}

	sumBody, err := fetchCapped(ctx, client, base+".sha256", 1<<10)
	if err != nil {
		return "", docsSiteFetchError(tag, err)
	}
	want, err := parseSHA256Line(sumBody)
	if err != nil {
		return "", fmt.Errorf("%s.sha256: %w", asset, err)
	}
	archive, err := fetchCapped(ctx, client, base, docsSiteMaxArchive)
	if err != nil {
		return "", docsSiteFetchError(tag, err)
	}
	got := sha256.Sum256(archive)
	if hex.EncodeToString(got[:]) != want {
		return "", fmt.Errorf("%s: checksum mismatch (got %x, release says %s); refusing to extract", asset, got, want)
	}

	tmp, err := cacheTempDir(root, "", tag)
	if err != nil {
		return "", err
	}
	if err := extractDocsSite(archive, root, tmp); err != nil {
		_ = root.RemoveAll(tmp)
		return "", fmt.Errorf("extract %s: %w", asset, err)
	}
	if err := swapIntoPlace(root, tmp, tag, refresh); err != nil {
		return "", fmt.Errorf("install docs site: %w", err)
	}
	return dest, nil
}

// cacheTempDir makes a fresh, randomly named directory under dir in the
// cache, for building a cache entry before it is renamed into place.
func cacheTempDir(root *os.Root, dir, tag string) (string, error) {
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", err
	}
	tmp := path.Join(dir, "."+tag+"-"+hex.EncodeToString(nonce[:]))
	if err := root.Mkdir(tmp, 0o700); err != nil {
		return "", fmt.Errorf("create temp directory: %w", err)
	}
	return tmp, nil
}

// swapIntoPlace renames the finished tmp entry to dest. replace discards
// an existing dest first, only now that its successor is complete, so a
// failed --refresh leaves the old copy usable. Without replace, a dest
// that appeared meanwhile is a concurrent run's equally good copy.
func swapIntoPlace(root *os.Root, tmp, dest string, replace bool) error {
	if replace {
		if err := root.RemoveAll(dest); err != nil {
			_ = root.RemoveAll(tmp)
			return err
		}
	}
	if err := root.Rename(tmp, dest); err != nil {
		_ = root.RemoveAll(tmp)
		if st, serr := root.Stat(dest); serr == nil && st.IsDir() {
			return nil
		}
		return err
	}
	return nil
}

// openCacheRoot creates the cache directory and opens it as an os.Root,
// so every cache read and write stays inside it.
func openCacheRoot(cacheRoot string) (*os.Root, error) {
	if err := os.MkdirAll(cacheRoot, 0o700); err != nil {
		return nil, fmt.Errorf("create cache directory: %w", err)
	}
	root, err := os.OpenRoot(cacheRoot)
	if err != nil {
		return nil, fmt.Errorf("open cache directory: %w", err)
	}
	return root, nil
}

// docsSiteAsset names the release asset; release.yml publishes the same
// name (TestReleaseAttachesDocsSite holds the two together).
func docsSiteAsset(tag string) string { return "gofastr-site-" + tag + ".tar.gz" }

var errNotFound = errors.New("not found")

func docsSiteFetchError(tag string, err error) error {
	if errors.Is(err, errNotFound) {
		hint := "try --full, or `gofastr docs` for the markdown"
		if semver.Compare(tag, docsFullMinRelease) < 0 {
			hint = "use `gofastr docs` for the markdown"
		}
		return fmt.Errorf("release %s has no docs-site archive (releases before docs serve shipped carry none); %s", tag, hint)
	}
	return fmt.Errorf("download docs site: %w", err)
}

// fetchCapped GETs url and returns at most limit bytes, failing when the
// body is larger.
func fetchCapped(ctx context.Context, client *http.Client, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%s: %w", url, errNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("%s: larger than %d bytes", url, limit)
	}
	return body, nil
}

// parseSHA256Line reads the `sha256sum` output format: the hex digest,
// then optionally whitespace and the file name.
func parseSHA256Line(body []byte) (string, error) {
	fields := strings.Fields(string(body))
	if len(fields) == 0 {
		return "", errors.New("empty checksum file")
	}
	sum := strings.ToLower(fields[0])
	if len(sum) != sha256.Size*2 {
		return "", fmt.Errorf("malformed checksum %q", fields[0])
	}
	if _, err := hex.DecodeString(sum); err != nil {
		return "", fmt.Errorf("malformed checksum %q", fields[0])
	}
	return sum, nil
}

// extractDocsSite unpacks a gzipped tar of regular files and directories
// into dir under cache. Writes go through os.Root, so no entry lands outside
// dir; links and device entries are refused outright since an export never
// has them.
func extractDocsSite(archive []byte, cache *os.Root, dir string) error {
	root, err := cache.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()

	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	var total int64
	entries := 0
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		entries++
		if entries > docsSiteMaxEntries {
			return fmt.Errorf("more than %d entries", docsSiteMaxEntries)
		}
		name, err := docsSiteEntryName(hdr.Name)
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if name == "." {
				continue
			}
			if err := root.MkdirAll(name, 0o700); err != nil {
				return err
			}
		case tar.TypeReg:
			if name == "." {
				return fmt.Errorf("entry %q: a file cannot be the archive root", hdr.Name)
			}
			total += hdr.Size
			if hdr.Size < 0 || total > docsSiteMaxExtract {
				return fmt.Errorf("extracted size exceeds %d bytes", docsSiteMaxExtract)
			}
			if d := path.Dir(name); d != "." {
				if err := root.MkdirAll(d, 0o700); err != nil {
					return err
				}
			}
			f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
			if err != nil {
				return err
			}
			n, cerr := io.Copy(f, io.LimitReader(tr, hdr.Size))
			if err := f.Close(); err != nil && cerr == nil {
				cerr = err
			}
			if cerr != nil {
				return cerr
			}
			if n != hdr.Size {
				return fmt.Errorf("entry %q: truncated", hdr.Name)
			}
		default:
			return fmt.Errorf("entry %q: type %q is not a regular file or directory", hdr.Name, hdr.Typeflag)
		}
	}
	if _, err := root.Stat("index.html"); err != nil {
		return errors.New("archive has no index.html at its root")
	}
	return nil
}

// docsSiteEntryName cleans a tar entry name to a root-relative slash path,
// refusing absolute names, parent segments and anything fs.ValidPath
// rejects.
func docsSiteEntryName(raw string) (string, error) {
	if strings.Contains(raw, "\\") || strings.HasPrefix(raw, "/") {
		return "", fmt.Errorf("entry %q: not a relative slash path", raw)
	}
	for _, seg := range strings.Split(raw, "/") {
		if seg == ".." {
			return "", fmt.Errorf("entry %q: parent segment", raw)
		}
	}
	name := path.Clean(raw)
	if !fs.ValidPath(name) {
		return "", fmt.Errorf("entry %q: invalid path", raw)
	}
	return name, nil
}

// serveDocsDir mounts an export on a gofastr app bound to loopback.
func serveDocsDir(dir string, opts docsServeOptions) error {
	app, err := newDocsSiteApp(dir)
	if err != nil {
		return err
	}
	app.OnReady(func(addr string) {
		url := "http://" + displayAddr(addr)
		success("Docs at %s (Ctrl-C to stop)", url)
		if opts.open {
			openBrowser(url)
		}
	})
	// Worktree isolation remaps a dev app's port so parallel checkouts do
	// not collide. The docs site is not the project's app, and the user
	// asked for this port, so serve it there.
	_ = os.Setenv("GOFASTR_ISOLATION", "off")
	return app.Start(net.JoinHostPort("127.0.0.1", strconv.Itoa(opts.port)))
}

// newDocsSiteApp builds the gofastr app that serves a static export.
func newDocsSiteApp(dir string) (*framework.App, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(filepath.Join(abs, "index.html")); err != nil {
		return nil, fmt.Errorf("%s has no index.html; is it a static export?", abs)
	}
	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "gofastr-docs"}))
	static.Mount(app.Router(), static.Config{FS: os.DirFS(abs), NotFoundFile: "404.html"})
	return app, nil
}

// displayAddr turns a bound listener address into the host:port a browser
// should open.
func displayAddr(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	if host == "" || host == "::" || host == "0.0.0.0" || host == "127.0.0.1" {
		host = "localhost"
	}
	return net.JoinHostPort(host, port)
}

// runFullDocsSite installs examples/site at tag with the Go toolchain
// (the module proxy serves the tagged source, verified against the
// checksum database) and runs it on loopback.
// docsFullMinRelease is the first release whose examples/site accepts
// a host:port PORT; older ones prepend ":" and fail to listen.
const docsFullMinRelease = "v0.82.0"

// portFree reports a listener already on addr.
func portFree(addr string, port int) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("port %d is in use (pick another with --port): %w", port, err)
	}
	return ln.Close()
}

func runFullDocsSite(ctx context.Context, cacheRoot, tag string, opts docsServeOptions) error {
	root, err := openCacheRoot(cacheRoot)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	name := "site"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	rel := path.Join("full", tag)
	bin := filepath.Join(cacheRoot, "full", tag, name)
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(opts.port))
	url := "http://" + displayAddr(addr)
	// The readiness wait below only sees that something accepts on addr,
	// so make sure nothing else already does: before the install, so a
	// taken port fails fast, and again after it, since an install takes
	// minutes.
	if err := portFree(addr, opts.port); err != nil {
		return err
	}
	if _, err := root.Stat(path.Join(rel, name)); opts.refresh || err != nil {
		if err := installFullDocsSite(ctx, root, cacheRoot, tag, name, opts.refresh); err != nil {
			return err
		}
		if err := portFree(addr, opts.port); err != nil {
			return err
		}
	}
	// Ctrl-C reaches the child through the terminal's process group; catch
	// it here too so the parent waits for the site's drain instead of
	// exiting first, and forward a SIGTERM sent to the parent alone.
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	cmd := exec.CommandContext(ctx, bin)
	// Isolation off for the same reason as the static server: the site
	// should listen on the port the user asked for, not a worktree remap.
	cmd.Env = append(os.Environ(), "PORT="+addr, "GOFASTR_ISOLATION=off")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 10 * time.Second
	if err := cmd.Start(); err != nil {
		return err
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	if waitForListener(ctx, addr, exited) {
		success("Docs at %s (Ctrl-C to stop)", url)
		if opts.open {
			openBrowser(url)
		}
	}
	err = <-exited
	if ctx.Err() != nil {
		return nil
	}
	return err
}

// installFullDocsSite runs go install into a temp directory and renames
// it into full/<tag>, so an interrupted install never leaves a partial
// binary that later runs would try to execute.
func installFullDocsSite(ctx context.Context, root *os.Root, cacheRoot, tag, name string, replace bool) error {
	if _, err := exec.LookPath("go"); err != nil {
		return errors.New("--full needs a Go toolchain on PATH")
	}
	if err := root.MkdirAll("full", 0o700); err != nil {
		return fmt.Errorf("create cache directory: %w", err)
	}
	tmp, err := cacheTempDir(root, "full", tag)
	if err != nil {
		return err
	}
	info("Building the %s docs site with go install (one time)…", tag)
	if err := goInstallSite(ctx, tag, filepath.Join(cacheRoot, filepath.FromSlash(tmp))); err != nil {
		_ = root.RemoveAll(tmp)
		return fmt.Errorf("go install examples/site@%s: %w", tag, err)
	}
	if _, err := root.Stat(path.Join(tmp, name)); err != nil {
		_ = root.RemoveAll(tmp)
		return fmt.Errorf("go install examples/site@%s produced no %s", tag, name)
	}
	if err := swapIntoPlace(root, tmp, path.Join("full", tag), replace); err != nil {
		return fmt.Errorf("install docs site: %w", err)
	}
	return nil
}

// waitForListener polls addr until it accepts a connection, reporting
// false when the process exits or ctx ends first. A process that exits
// leaves its result in exited for the caller to read.
func waitForListener(ctx context.Context, addr string, exited chan error) bool {
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	for {
		if conn, err := net.DialTimeout("tcp", addr, time.Second); err == nil {
			_ = conn.Close()
			return true
		}
		select {
		case err := <-exited:
			exited <- err
			return false
		case <-ctx.Done():
			return false
		case <-tick.C:
		}
	}
}
