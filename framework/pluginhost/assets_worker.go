package pluginhost

import "fmt"

// WorkerCSP is the CSP of a trusted worker's own script response.
type WorkerCSP struct {
	// ScriptKeywords: 'unsafe-eval' or 'wasm-unsafe-eval'.
	ScriptKeywords []string

	// ConnectSources: only 'self'.
	ConnectSources []string

	// WASM adds 'wasm-unsafe-eval' to script-src.
	WASM bool
}

var allowedWorkerKeywords = map[string]bool{
	"'unsafe-eval'":      true,
	"'wasm-unsafe-eval'": true,
}

var allowedWorkerConnectSources = map[string]bool{
	"'self'": true,
}

func validateWorkerProfile(p WorkerCSP) error {
	for _, kw := range p.ScriptKeywords {
		if !allowedWorkerKeywords[kw] {
			return fmt.Errorf("pluginhost: worker csp script keyword %q is not in the allowlist (only 'unsafe-eval' and 'wasm-unsafe-eval' are permitted)", kw)
		}
	}
	for _, src := range p.ConnectSources {
		if !allowedWorkerConnectSources[src] {
			return fmt.Errorf("pluginhost: worker csp connect source %q is not in the allowlist (only 'self' is permitted; serve pinned runtimes and models from your own origin)", src)
		}
	}
	return nil
}

func workerCSP(p WorkerCSP) string {
	scriptSrc := "'self'"
	seen := map[string]bool{}
	add := func(kw string) {
		if seen[kw] {
			return
		}
		seen[kw] = true
		scriptSrc += " " + kw
	}
	if p.WASM {
		add("'wasm-unsafe-eval'")
	}
	for _, kw := range p.ScriptKeywords {
		if !allowedWorkerKeywords[kw] {
			continue
		}
		add(kw)
	}
	connect := "'none'"
	for _, src := range p.ConnectSources {
		if allowedWorkerConnectSources[src] {
			connect = src
		}
	}
	return "default-src 'self'" +
		"; script-src " + scriptSrc +
		"; connect-src " + connect +
		"; worker-src 'self'" +
		"; object-src 'none'"
}

// CacheProfile names the Cache-Control posture of a byte-backed asset.
type CacheProfile int

const (
	// CacheDefault sends "no-store, max-age=0".
	CacheDefault CacheProfile = iota

	// CachePublicImmutable is for content-hashed, secret-free URLs only.
	CachePublicImmutable

	// CachePrivateRevalidate sends "private, no-cache".
	CachePrivateRevalidate

	// CachePrivateNoStore sends "private, no-store".
	CachePrivateNoStore
)

func (c CacheProfile) cacheControl() string {
	switch c {
	case CachePublicImmutable:
		return "public, max-age=31536000, immutable"
	case CachePrivateRevalidate:
		return "private, no-cache"
	case CachePrivateNoStore:
		return "private, no-store"
	default:
		return "no-store, max-age=0"
	}
}

func (c CacheProfile) valid() bool {
	return c >= CacheDefault && c <= CachePrivateNoStore
}

type assetOptions struct {
	worker *WorkerCSP
	cache  CacheProfile
}

// AssetOption customises one asset registered with [AssetServer.AddBytes].
type AssetOption func(*assetOptions)

// WithWorkerCSP marks the asset as a trusted worker with profile p.
func WithWorkerCSP(p WorkerCSP) AssetOption {
	return func(o *assetOptions) { o.worker = &p }
}

// WithCache sets the asset's cache posture; the default is [CacheDefault].
func WithCache(c CacheProfile) AssetOption {
	return func(o *assetOptions) { o.cache = c }
}
