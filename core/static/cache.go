package static

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"
)

type digestKey struct {
	name    string
	modTime int64
	size    int64
	ino     uint64
	ctime   int64
}

func keyOf(name string, stat fs.FileInfo) digestKey {
	ino, ctime := fileRevision(stat)
	return digestKey{name: name, modTime: stat.ModTime().UnixNano(), size: stat.Size(), ino: ino, ctime: ctime}
}

func (k digestKey) settled() bool {
	d := time.Since(time.Unix(0, k.ctime))
	return k.ctime == 0 || d >= time.Second || d < 0
}

type digestCache struct {
	done     sync.Map
	inflight sync.Map
	slots    chan struct{}
}

func newDigestCache() *digestCache {
	return &digestCache{slots: make(chan struct{}, backgroundHashSlots)}
}

const largeFileETagThreshold = 16 << 20

const backgroundHashSlots = 2

func fileETag(cache *digestCache, f fs.File, stat fs.FileInfo, name string, reopen func() (fs.File, error)) (string, bool, error) {
	key := keyOf(name, stat)
	settled := key.settled()
	if cache != nil {
		if v, ok := cache.done.Load(key); ok && settled {
			return v.(string), false, nil
		}
		if stat.Size() > largeFileETagThreshold {
			cache.hashInBackground(key, reopen)
			return "", false, nil
		}
	}
	etag, err := hashETag(f)
	if err != nil {
		return "", true, err
	}
	if cache != nil && settled {
		cache.done.Store(key, etag)
	}
	return etag, true, nil
}

func hashETag(r io.Reader) (string, error) {
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}
	return `"` + hex.EncodeToString(h.Sum(nil)[:16]) + `"`, nil
}

func (c *digestCache) hashInBackground(key digestKey, open func() (fs.File, error)) {
	if _, running := c.inflight.LoadOrStore(key, struct{}{}); running {
		return
	}
	select {
	case c.slots <- struct{}{}:
	default:
		c.inflight.Delete(key)
		return
	}
	release := func() {
		<-c.slots
		c.inflight.Delete(key)
	}
	f, err := openRecovered(open)
	if err != nil {
		release()
		return
	}
	go func() {
		defer release()
		defer func() { _ = recover() }()
		defer f.Close()
		if !key.settled() {
			time.Sleep(time.Second)
			if !key.settled() {
				return
			}
		}
		if !sameRevision(f, key) {
			return
		}
		etag, err := hashETag(f)
		if err != nil || !sameRevision(f, key) {
			return
		}
		c.done.Store(key, etag)
	}()
}

func openRecovered(open func() (fs.File, error)) (f fs.File, err error) {
	defer func() {
		if r := recover(); r != nil {
			f, err = nil, fs.ErrInvalid
		}
	}()
	f, err = open()
	if err == nil && f == nil {
		err = fs.ErrNotExist
	}
	return f, err
}

func sameRevision(f fs.File, key digestKey) bool {
	st, err := f.Stat()
	return err == nil && keyOf(key.name, st) == key
}

func setCacheHeaders(w http.ResponseWriter, etag string, modTime time.Time, maxAge time.Duration) {
	if maxAge > 0 {
		w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", int(maxAge.Seconds())))
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}

	if etag != "" {
		w.Header().Set("ETag", etag)
	}

	if !modTime.IsZero() {
		w.Header().Set("Last-Modified", modTime.UTC().Format(http.TimeFormat))
	}
}

func preconditionStatus(r *http.Request, etag string, modTime time.Time) int {
	if im := r.Header.Get("If-Match"); im != "" {
		if !etagListMatch(im, etag, false) {
			return http.StatusPreconditionFailed
		}
	} else if ius := r.Header.Get("If-Unmodified-Since"); ius != "" && !modTime.IsZero() {
		if t, err := http.ParseTime(ius); err == nil && modTime.Truncate(time.Second).After(t) {
			return http.StatusPreconditionFailed
		}
	}
	if inm := r.Header.Get("If-None-Match"); inm != "" {
		if etagListMatch(inm, etag, true) {
			return http.StatusNotModified
		}
		return 0
	}
	if ims := r.Header.Get("If-Modified-Since"); ims != "" && !modTime.IsZero() {
		if t, err := http.ParseTime(ims); err == nil && !modTime.Truncate(time.Second).After(t) {
			return http.StatusNotModified
		}
	}
	return 0
}

// IfNoneMatch reports whether r's If-None-Match list names etag or "*",
// comparing weakly.
func IfNoneMatch(r *http.Request, etag string) bool {
	return etagListMatch(r.Header.Get("If-None-Match"), etag, true)
}

func etagListMatch(list, etag string, weak bool) bool {
	for _, tag := range strings.Split(list, ",") {
		tag = strings.TrimSpace(tag)
		if tag == "*" {
			return true
		}
		if etag == "" || tag == "" {
			continue
		}
		if weak {
			if strings.TrimPrefix(tag, "W/") == strings.TrimPrefix(etag, "W/") {
				return true
			}
		} else if !strings.HasPrefix(tag, "W/") && tag == etag {
			return true
		}
	}
	return false
}

// FingerprintURL inserts hash before filePath's extension:
// "/assets/app.js" becomes "/assets/app.<hash>.js".
func FingerprintURL(filePath, hash string) string {
	ext := path.Ext(filePath)
	base := strings.TrimSuffix(filePath, ext)
	return base + "." + hash + ext
}
