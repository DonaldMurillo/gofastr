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

	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

func (b *Builder) dumpPWAAssets(res *Result, frameworkAssetCount int, written *fileSet) error {
	if !b.Host.PWAEnabled() {
		return nil
	}
	manifest, err := b.Host.PWAManifestJSON(context.Background(), b.BasePath)
	if err != nil {
		return fmt.Errorf("static: pwa manifest: %w", err)
	}
	type pwaFile struct {
		urlPath string
		relFile string
		body    []byte
	}
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
	const pwaDir = "__gofastr/pwa"
	generated := map[string]bool{
		"manifest.webmanifest":  true,
		"service-worker.js":     true,
		pwaDir + "/register.js": true,
		pwaOfflineFile:          true,
	}
	for _, f := range localeManifests {
		generated[filepath.ToSlash(f.relFile)] = true
	}
	genPaths := slices.Sorted(maps.Keys(generated))
	if err := respell(b.OutDir, genPaths); err != nil {
		return err
	}
	var genFiles []fs.FileInfo
	for _, g := range genPaths {
		if b.staticSourceHas(g) {
			delete(generated, g)
			continue
		}
		if fi, err := os.Stat(filepath.Join(b.OutDir, filepath.FromSlash(g))); err == nil {
			genFiles = append(genFiles, fi)
		}
	}
	contentHash, err := hashExportTree(b.OutDir, func(rel string) bool {
		if generated[rel] {
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
	offline := b.slashPageLinks(b.rewriteBaseURLs(stampStatic(b.Host.PWAOfflineHTML())), pageSet(res.Pages))
	offline = applyStaticCSP(offline, b.policy())
	files := []pwaFile{
		{"/manifest.webmanifest", "manifest.webmanifest", manifest},
		{"/service-worker.js", "service-worker.js", []byte(sw)},
		{pwaRegisterJS, filepath.Join("__gofastr", "pwa", "register.js"), []byte(b.Host.PWARegisterJS(b.BasePath))},
		{pwaOfflinePath, filepath.FromSlash(pwaOfflineFile), []byte(offline)},
	}
	files = append(files, localeManifests...)
	for _, f := range files {
		if !generated[filepath.ToSlash(f.relFile)] {
			b.log("kept user-supplied %s", f.urlPath)
			continue
		}
		dst := filepath.Join(b.OutDir, f.relFile)
		if err := writeUnder(b.OutDir, dst, f.body); err != nil {
			return err
		}
		res.Assets = append(res.Assets, f.urlPath)
		written.add(filepath.ToSlash(f.relFile))
		b.log("wrote %s", f.urlPath)
	}
	return nil
}

func escapedURLs(paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = (&url.URL{Path: p}).EscapedPath()
	}
	return out
}

const (
	pwaRegisterJS  = "/__gofastr/pwa/register.js"
	pwaOfflinePath = "/__gofastr/pwa/offline"
	pwaOfflineFile = "__gofastr/pwa/offline/index.html"
)

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
