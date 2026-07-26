package image

import "sync"

// defaultBlurHashQuality is the JPEG quality BlurHashDataURL uses when
// BlurHashRenderConfig.Quality is zero. A decoded blur is nothing but
// low-frequency gradients, so quality below ~50 starts showing blocking
// artifacts for no meaningful byte saving — measured across several
// hashes, q40 saved ~12 bytes on an ~880-byte payload.
//
// JPEG is the default format rather than PNG because its size is nearly
// flat in the output dimensions (a JPEG's quantisation and Huffman tables
// dominate a payload this small): ~840–940 bytes anywhere from 16 px to
// 48 px. PNG is smaller at or below 20 px (~430–880 bytes) but scales with
// pixel count and reaches ~2.4 KB by 48 px, so it punishes a caller who
// raises Width. Callers who want the smaller payload at the default size,
// or lossless output for flat-color content, set Format: FormatPNG.
const defaultBlurHashQuality = 50

// defaultBlurHashCacheSize is the number of rendered placeholders kept in
// memory. A page renders one placeholder per image, and a list view
// re-renders the same handful of hashes on every request, so a few hundred
// entries covers realistic working sets at a few hundred bytes each.
const defaultBlurHashCacheSize = 512

// BlurHashDataURL renders a BlurHash string into a data: URL ready to hand
// to the UI layer as a placeholder:
//
//	durl, _ := image.BlurHashDataURL(row.CoverBlurHash, image.BlurHashRenderConfig{})
//	ui.PipelineImage(ui.PipelineImageConfig{Placeholder: durl, …})
//
// This is the render-time counterpart to Image.BlurHash: store the ~28-char
// hash in a column at upload time, call this to turn it back into pixels.
// Results are memoised, since the same handful of hashes recur on every
// request for a given page.
//
// A returned error means the hash was malformed; callers rendering
// user-supplied data should treat a placeholder as optional and fall back
// to no placeholder rather than failing the page.
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
	// Only successful renders are cached — a malformed hash must not
	// occupy a slot, or a hot bad row would evict good entries.
	blurHashCache.put(k, durl)
	return durl, nil
}

// FlushBlurHashCache drops every memoised placeholder. Intended for tests
// and for long-lived processes that want to reclaim the memory; correctness
// never depends on calling it, since the cache keys a pure function.
func FlushBlurHashCache() { blurHashCache.flush() }

// SetBlurHashCacheSize caps how many rendered placeholders are retained.
// A value <= 0 disables caching entirely. Shrinking the cap drops existing
// entries beyond it.
func SetBlurHashCacheSize(n int) { blurHashCache.resize(n) }

// BlurHashCacheLen reports how many placeholders are currently memoised.
func BlurHashCacheLen() int { return blurHashCache.len() }

// blurHashKey identifies a rendered placeholder. Every field of
// BlurHashRenderConfig that changes the output bytes participates —
// omitting one would serve one variant's render for another's request.
type blurHashKey struct {
	hash    string
	width   int
	height  int
	punch   float64
	format  Format
	quality int
}

var blurHashCache = &blurHashMemo{cap: defaultBlurHashCacheSize}

// blurHashMemo is a bounded memo table for a pure function. Eviction is
// arbitrary rather than LRU on purpose: entries are interchangeable
// (any miss simply recomputes), and the map-iteration order Go already
// randomises gives an adequate victim without the bookkeeping an
// access-ordered list would need on every read.
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
	// Evict before inserting so the table never exceeds cap. A concurrent
	// racer may have filled it past cap-1 between our get and this lock,
	// hence the loop rather than a single delete.
	for len(m.entries) >= m.cap {
		if !m.evictOne() {
			return
		}
	}
	m.entries[k] = v
}

// evictOne removes an arbitrary entry, reporting whether one was removed.
// Caller must hold the write lock.
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
