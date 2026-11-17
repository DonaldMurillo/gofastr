package static

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// dumpPWAAssets emits the WithPWA surface for a serverless deploy:
// manifest.webmanifest, service-worker.js, the registration script, and
// the offline fallback page (as offline/index.html so a plain static
// server resolves the extension-less precache URL). No-op when the host
// did not opt into WithPWA.
// Each file it writes is added to written under its file path (the
// offline page's URL is a directory).
func (b *Builder) dumpPWAAssets(res *Result, frameworkAssetCount int, written *fileSet) error {
	if !b.Host.PWAEnabled() {
		return nil
	}
	manifest, err := b.Host.PWAManifestJSON(context.Background(), b.BasePath)
	if err != nil {
		return fmt.Errorf("static: pwa manifest: %w", err)
	}
	// Static exports get the full-site worker: the page set is closed and
	// immutable, so every exported page and asset is precached at install
	// and the whole site works offline. The tree hash makes a
	// content-only redeploy rotate the cache version.
	type pwaFile struct {
		urlPath string
		relFile string
		body    []byte
	}
	// A locale tree's pages link their own manifest, whose start_url
	// launches into that tree.
	var localeManifests []pwaFile
	if locales, byPrefix := b.Host.PathLocales(); byPrefix {
		for _, tag := range locales.Prefixed {
			body, err := b.Host.PWAManifestJSON(localeContext(context.Background(), locales, tag), b.BasePath)
			if err != nil {
				return fmt.Errorf("static: pwa manifest %s: %w", tag, err)
			}
			localeManifests = append(localeManifests, pwaFile{locales.Path(tag, "/manifest.webmanifest"), filepath.Join(tag, "manifest.webmanifest"), body})
		}
	}
	// The hash leaves out every file this surface writes: a rebuild
	// finds them from the last build and a fresh build does not. They
	// are respelled first, so a leftover in another case that is the
	// same file on this disk (MANIFEST.webmanifest on APFS) takes the
	// generated name and drops out, and one that is a file of its own
	// on a case-sensitive disk stays in the hash like any other file.
	generated := map[string]bool{"manifest.webmanifest": true, "service-worker.js": true}
	for _, f := range localeManifests {
		generated[filepath.ToSlash(f.relFile)] = true
	}
	const pwaDir = "__gofastr/pwa"
	genPaths := append(slices.Sorted(maps.Keys(generated)), pwaDir+"/register.js", pwaDir+"/offline/index.html")
	if err := respell(b.OutDir, genPaths); err != nil {
		return err
	}
	// An alias of a generated file (a symlink or hard link under any
	// name) would hash the last build's copy, and change with every
	// build: it is left out like the file it names. A file the static
	// source supplies is the user's, not generated, and its aliases
	// stay in the hash.
	var genFiles []fs.FileInfo
	for _, g := range genPaths {
		if b.staticSourceHas(g) {
			continue
		}
		if fi, err := os.Stat(filepath.Join(b.OutDir, filepath.FromSlash(g))); err == nil {
			genFiles = append(genFiles, fi)
		}
	}
	contentHash, err := hashExportTree(b.OutDir, func(rel string) bool {
		if generated[rel] || strings.HasPrefix(rel, pwaDir+"/") {
			return true
		}
		fi, err := os.Stat(filepath.Join(b.OutDir, filepath.FromSlash(rel))) //gofastr:allow(rootread) rel is enumerated from hashExportTree's own WalkDir of OutDir — nothing caller-controlled — and following a leaf alias is the point: os.SameFile detects generated-file links
		return err == nil && slices.ContainsFunc(genFiles, func(g fs.FileInfo) bool { return os.SameFile(fi, g) })
	})
	if err != nil {
		return fmt.Errorf("static: fingerprint export: %w", err)
	}
	if frameworkAssetCount > len(res.Assets) {
		frameworkAssetCount = len(res.Assets)
	}
	// Pages are precached under the URL their links name (see
	// slashPageLinks): a client navigation is a plain fetch, which the
	// worker matches exactly, so /about would never answer /about/.
	// The worker fetches and matches escaped URLs: a page or file named
	// "a?b" precached raw is fetched as /a with a query, its 404 fails
	// the whole install, and a navigation never matches it.
	pages := make([]string, len(res.Pages))
	for i, p := range res.Pages {
		pages[i] = slashed(coreapp.EscapeRoutePath(p))
	}
	sw, err := b.Host.PWAStaticServiceWorkerJS(b.BasePath, uihost.PWAStaticExport{
		Pages:          pages,
		Assets:         escapedURLs(res.Assets[:frameworkAssetCount]),
		OptionalAssets: escapedURLs(res.Assets[frameworkAssetCount:]),
		ContentHash:    contentHash,
	})
	if err != nil {
		return fmt.Errorf("static: pwa service worker: %w", err)
	}
	// The offline page is rendered basePath-neutral by the host; its
	// asset links get the same base rewrite as every exported page. It
	// also gets the data-cui-static stamp so the runtime's static-mode
	// guards apply, but not the "run locally" banner. An offline
	// fallback is not a demo page. Its page links take the slash the
	// pages' own do (slashPageLinks), and it carries the export's CSP
	// meta like every page: a host that ignores _headers serves it with
	// no other policy.
	offline := b.slashPageLinks(b.rewriteBaseURLs(stampStatic(b.Host.PWAOfflineHTML())), pageSet(res.Pages))
	offline = applyStaticCSP(offline, b.policy())
	files := []pwaFile{
		{"/manifest.webmanifest", "manifest.webmanifest", manifest},
		{"/service-worker.js", "service-worker.js", []byte(sw)},
		{pwaRegisterJS, filepath.Join("__gofastr", "pwa", "register.js"), []byte(b.Host.PWARegisterJS(b.BasePath))},
		{"/__gofastr/pwa/offline", filepath.Join("__gofastr", "pwa", "offline", "index.html"), []byte(offline)},
	}
	files = append(files, localeManifests...)
	for _, f := range files {
		// User-supplied files win: before this surface moved to the end of
		// the build, a manifest/worker shipped in the app's static dir
		// landed after (and therefore over) the generated one. Preserve
		// that precedence by never clobbering a file the static SOURCE
		// provides (the source, not OutDir: a previous build's leftovers
		// in a reused OutDir must not suppress regeneration).
		if b.staticSourceHas(f.relFile) {
			b.log("kept user-supplied %s", f.urlPath)
			continue
		}
		dst := filepath.Join(b.OutDir, f.relFile)
		if err := b.ensureContained(dst); err != nil {
			return err
		}
		if err := writeUnder(b.OutDir, dst, f.body); err != nil {
			return err
		}
		res.Assets = append(res.Assets, f.urlPath)
		written.add(filepath.ToSlash(f.relFile))
		b.log("wrote %s", f.urlPath)
	}
	return nil
}

// escapedURLs is the request URL of each written file: Result.Assets
// holds the decoded path a static host resolves to the file.
func escapedURLs(paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = (&url.URL{Path: p}).EscapedPath()
	}
	return out
}

// pwaRegisterJS is the WithPWA registration script, on the rail and
// written by dumpPWAAssets.
const pwaRegisterJS = "/__gofastr/pwa/register.js"

// hashExportTree fingerprints every file already written to outDir
// (deterministic sorted walk over relative path + streamed bytes). The
// PWA outputs themselves are excluded: they are (re)generated after
// this hash, and a reused OutDir still holds the PREVIOUS build's
// copies; hashing those would make every rebuild byte-different even
// with identical content. A content-only redeploy therefore changes
// the hash → the worker bytes → the cache version, and installed
// clients re-precache; an unchanged rebuild reproduces the same worker.
//
// skip reports, for a slash path under outDir, whether it is left out.
func hashExportTree(outDir string, skip func(string) bool) (string, error) {
	h := sha256.New()
	err := filepath.WalkDir(outDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(outDir, path)
		if err != nil {
			return err
		}
		slashRel := filepath.ToSlash(rel)
		if skip(slashRel) {
			return nil
		}
		h.Write([]byte(slashRel))
		h.Write([]byte{0})
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		_, err = io.Copy(h, f)
		_ = f.Close()
		if err != nil {
			return err
		}
		h.Write([]byte{0})
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil))[:16], nil
}
