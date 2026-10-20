package uihost

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"net/http"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/dev"
)

// immutableCacheControl is what a static file served at its current
// content version carries: the URL changes whenever the bytes do, so a
// browser or CDN may keep it for a year without revalidating.
const immutableCacheControl = "public, max-age=31536000, immutable"

// assetVersions caches the content hash of static files by source and
// path, keyed by the modification time and size the hash was taken at, so
// an edited file is hashed again and an unchanged one is not.
type assetVersions struct {
	mu      sync.Mutex
	entries map[assetKey]assetVersion
}

type assetKey struct {
	fromFS bool // the file came from the static FS, not the static dir
	rel    string
}

type assetVersion struct {
	modTime time.Time
	size    int64
	hash    string
}

// AssetURL returns the URL of a file in the static directory or static FS
// with its content hash appended, e.g. "/logo.svg?v=3f2a9c01b7d4". The host
// serves a request whose v matches the file's current hash with a
// year-long immutable Cache-Control, so browsers and CDNs keep the file
// until its bytes change, and a deploy that changes it changes the URL.
//
// p must be a clean rooted path ("/css/site.css"). Anything else, and a
// path with no file behind it, comes back unchanged, so a typo renders a
// URL that 404s rather than one that hides the mistake. The hash is cached
// per file and taken again when the file's modification time or size
// changes, so under `gofastr dev` an edit shows on the next render.
func (ds *UIHost) AssetURL(p string) string {
	if p == "" || p == "/" || !strings.HasPrefix(p, "/") || strings.ContainsAny(p, "?#") || path.Clean(p) != p {
		return p
	}
	if hash := ds.assetHash(strings.TrimPrefix(p, "/")); hash != "" {
		return p + "?v=" + hash
	}
	return p
}

// assetHash resolves rel the way serveOrRender does (static dir first,
// then the static FS) and returns its content hash, or "" when no file is
// there or it cannot be read.
func (ds *UIHost) assetHash(rel string) string {
	if root := ds.staticRootOf(); root != nil {
		if f, err := root.Open(filepath.FromSlash(rel)); err == nil {
			hash, isFile := ds.versions.hashOpen(assetKey{rel: rel}, f)
			_ = f.Close()
			if isFile {
				return hash
			}
		}
	}
	if ds.staticFS != nil {
		if f, err := ds.staticFS.Open(rel); err == nil {
			hash, _ := ds.versions.hashOpen(assetKey{fromFS: true, rel: rel}, f)
			_ = f.Close()
			return hash
		}
	}
	return ""
}

// hashOpen hashes an open file, stamped by that same handle's Stat, so the
// stamp and the hash always describe one version of the file even while
// it is being replaced. isFile is false for a directory or a failed Stat.
func (v *assetVersions) hashOpen(key assetKey, f fs.File) (hash string, isFile bool) {
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		return "", false
	}
	return v.lookup(key, info, f), true
}

// lookup returns the hash cached for key at info's stamp, or reads r,
// the open file info describes, to take it.
func (v *assetVersions) lookup(key assetKey, info fs.FileInfo, r io.Reader) string {
	v.mu.Lock()
	e, ok := v.entries[key]
	v.mu.Unlock()
	if ok && e.modTime.Equal(info.ModTime()) && e.size == info.Size() {
		return e.hash
	}
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return ""
	}
	sum := h.Sum(nil)
	hash := hex.EncodeToString(sum[:6])
	v.mu.Lock()
	if v.entries == nil {
		v.entries = map[assetKey]assetVersion{}
	}
	v.entries[key] = assetVersion{modTime: info.ModTime(), size: info.Size(), hash: hash}
	v.mu.Unlock()
	return hash
}

// wantsVersion reports whether a static response may be marked immutable:
// the URL carries a ?v= and the host is not under `gofastr dev`, where
// devStaticNoStore keeps every static response uncacheable.
func wantsVersion(r *http.Request) bool {
	return !dev.Enabled() && r.URL.Query().Get("v") != ""
}

// markVersionedStatic sets the immutable Cache-Control on a static
// response whose ?v= names the content hash of f, the handle the response
// is served from, and then rewinds f. Hashing that handle rather than the
// path keeps a file replaced mid-request from certifying bytes this
// response does not carry. An unversioned or stale URL keeps the default
// revalidating behavior.
func (ds *UIHost) markVersionedStatic(w http.ResponseWriter, r *http.Request, key assetKey, f fs.File) {
	if !wantsVersion(r) {
		return
	}
	rs, ok := f.(io.Seeker)
	if !ok {
		return
	}
	hash, isFile := ds.versions.hashOpen(key, f)
	if _, err := rs.Seek(0, io.SeekStart); err != nil {
		return
	}
	if isFile && hash != "" && r.URL.Query().Get("v") == hash {
		w.Header().Set("Cache-Control", immutableCacheControl)
	}
}
