package image

import "sync"

const defaultBlurHashQuality = 50

const defaultBlurHashCacheSize = 512

// BlurHashDataURL renders a BlurHash string into a memoised data: URL.
func BlurHashDataURL(hash string, cfg BlurHashRenderConfig) (string, error) {
	k := blurHashKey{
		hash:    hash,
		width:   cfg.Width,
		height:  cfg.Height,
		punch:   cfg.Punch,
		format:  cfg.Format,
		quality: cfg.Quality,
	}
	if durl, ok := blurHashCache.get(k); ok {
		return durl, nil
	}

	img, err := DecodeBlurHash(hash, cfg)
	if err != nil {
		return "", err
	}
	var durl string
	if cfg.Format == FormatPNG {
		durl, err = img.PNG().DataURL()
	} else {
		q := cfg.Quality
		if q <= 0 {
			q = defaultBlurHashQuality
		}
		durl, err = img.JPEG(JPEGOptions{Quality: q}).DataURL()
	}
	if err != nil {
		return "", err
	}
	blurHashCache.put(k, durl)
	return durl, nil
}

// FlushBlurHashCache drops every memoised placeholder.
func FlushBlurHashCache() { blurHashCache.flush() }

// SetBlurHashCacheSize caps the memo; 0 or less disables it.
func SetBlurHashCacheSize(n int) { blurHashCache.resize(n) }

// BlurHashCacheLen reports how many placeholders are currently memoised.
func BlurHashCacheLen() int { return blurHashCache.len() }

type blurHashKey struct {
	hash    string
	width   int
	height  int
	punch   float64
	format  Format
	quality int
}

var blurHashCache = &blurHashMemo{cap: defaultBlurHashCacheSize}

type blurHashMemo struct {
	mu      sync.RWMutex
	entries map[blurHashKey]string
	cap     int
}

func (m *blurHashMemo) get(k blurHashKey) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.entries[k]
	return v, ok
}

func (m *blurHashMemo) put(k blurHashKey, v string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cap <= 0 {
		return
	}
	if m.entries == nil {
		m.entries = make(map[blurHashKey]string, m.cap)
	}
	for len(m.entries) >= m.cap {
		if !m.evictOne() {
			return
		}
	}
	m.entries[k] = v
}

func (m *blurHashMemo) evictOne() bool {
	for k := range m.entries {
		delete(m.entries, k)
		return true
	}
	return false
}

func (m *blurHashMemo) flush() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = nil
}

func (m *blurHashMemo) resize(n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cap = n
	if n <= 0 {
		m.entries = nil
		return
	}
	for len(m.entries) > n {
		if !m.evictOne() {
			return
		}
	}
}

func (m *blurHashMemo) len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.entries)
}
