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

const immutableCacheControl = "public, max-age=31536000, immutable"

type assetVersions struct {
	mu      sync.Mutex
	entries map[assetKey]assetVersion
}

type assetKey struct {
	fromFS bool
	rel    string
}

type assetVersion struct {
	modTime time.Time
	size    int64
	ino     uint64
	ctime   int64
	hash    string
}

// AssetURL returns p, a clean rooted path to a static file, with
// "?v=<content hash>" appended; any other p, or one with no file behind
// it, comes back unchanged.
func (ds *UIHost) AssetURL(p string) string {
	if p == "" || p == "/" || !strings.HasPrefix(p, "/") || strings.ContainsAny(p, "?#") || path.Clean(p) != p {
		return p
	}
	if hash := ds.assetHash(strings.TrimPrefix(p, "/")); hash != "" {
		return p + "?v=" + hash
	}
	return p
}

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

func (v *assetVersions) hashOpen(key assetKey, f fs.File) (hash string, isFile bool) {
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		return "", false
	}
	return v.lookup(key, info, f), true
}

func (v *assetVersions) lookup(key assetKey, info fs.FileInfo, r io.Reader) string {
	ino, ctime := assetFileRevision(info)
	d := time.Since(time.Unix(0, ctime))
	settled := ctime == 0 || d >= time.Second || d < 0
	v.mu.Lock()
	e, ok := v.entries[key]
	v.mu.Unlock()
	if settled && ok && e.modTime.Equal(info.ModTime()) && e.size == info.Size() && e.ino == ino && e.ctime == ctime {
		return e.hash
	}
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return ""
	}
	sum := h.Sum(nil)
	hash := hex.EncodeToString(sum[:6])
	if !settled {
		return hash
	}
	v.mu.Lock()
	if v.entries == nil {
		v.entries = map[assetKey]assetVersion{}
	}
	v.entries[key] = assetVersion{modTime: info.ModTime(), size: info.Size(), ino: ino, ctime: ctime, hash: hash}
	v.mu.Unlock()
	return hash
}

func wantsVersion(r *http.Request) bool {
	return !dev.Enabled() && r.URL.Query().Get("v") != ""
}

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
