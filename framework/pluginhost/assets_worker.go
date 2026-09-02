package pluginhost

import "fmt"

// WorkerCSP is the validated Content-Security-Policy profile for a trusted
// host-page worker's OWN script response. It is the third asset shape an
// [AssetServer] serves, beside host scripts (no policy of their own) and
// framed plugin assets ([framedCSP]): a worker the app compiles in and
// vouches for — an OpenCV or ONNX depth worker, say — running heavyweight
// code that needs runtime compilation the host document must never grant.
//
// The profile widens ONLY the worker's own response. A dedicated worker
// enforces the CSP delivered with its script, not the document's, so
// 'unsafe-eval' can live on the worker response alone while the host page
// keeps the app's strict policy byte-for-byte. Register one with
// [AssetServer.AddBytes] and framed=false:
//
//	srv.AddBytes("/__w/depth.js", "text/javascript; charset=utf-8", false, workerJS,
//		WithWorkerCSP(WorkerCSP{
//			ScriptKeywords: []string{"'unsafe-eval'"},
//			ConnectSources: []string{"'self'"},
//			WASM:           true,
//		}),
//		WithCache(CachePrivateNoStore))
//
// Every field is matched byte-for-byte against a closed allowlist at
// registration; a token carrying ';', whitespace, a host source, or a
// wildcard is rejected there and dropped again at assembly (see
// [workerCSP]) — the same double gate [Manifest.CSP] and framedCSP use.
type WorkerCSP struct {
	// ScriptKeywords appends keywords to the worker policy's script-src.
	// Allowlist: 'unsafe-eval' (runtime compilation — ONNX Runtime glue,
	// asm.js builds, and the wasm gate on browsers that predate
	// 'wasm-unsafe-eval') and 'wasm-unsafe-eval' (WebAssembly compilation
	// only, the narrower grant when no string eval is needed). The base
	// 'self' is always present and cannot be removed.
	ScriptKeywords []string

	// ConnectSources widens the worker policy's connect-src from its
	// 'none' default. Allowlist: 'self', for fetching the wasm binary and
	// model bytes from the app's own origin. A CDN, wildcard, or remote
	// endpoint is refused: applications pin their runtimes same-origin
	// (the plugin-platform doc carries the delivery recipe), and this
	// server is not a remote-artifact proxy.
	ConnectSources []string

	// WASM appends 'wasm-unsafe-eval' — WebAssembly compilation without
	// string eval. Prefer it over ScriptKeywords when the worker only
	// compiles; add 'unsafe-eval' only when the runtime actually needs it.
	WASM bool
}

// allowedWorkerKeywords is the closed set of Content-Security-Policy source
// keywords a trusted worker may append to its own script-src. Two members,
// both deliberate:
//
//   - 'wasm-unsafe-eval': WebAssembly compilation and nothing else — the
//     same grant the framed tier exposes, applied to a trusted same-origin
//     worker instead of an opaque frame.
//   - 'unsafe-eval': string eval and new Function, which is what runtimes
//     that generate code on the fly require (ONNX Runtime, asm.js builds).
//     It is a much bigger grant than wasm compilation, which is exactly why
//     it is available ONLY on a worker response the app registered itself,
//     never on the host document and never on a framed asset.
//
// Exact byte-for-byte matching, like [allowedCSPKeywords]: these values are
// interpolated into a response header, so anything carrying ';', embedded
// whitespace, or mismatched quotes could splice a directive (re-enabling
// connect-src, the network guard). Host sources, 'unsafe-inline', 'data:',
// and '*' stay out on purpose: a worker loads its own bytes and, when it
// opted in, same-origin artifacts — nothing else.
var allowedWorkerKeywords = map[string]bool{
	"'unsafe-eval'":      true,
	"'wasm-unsafe-eval'": true,
}

// allowedWorkerConnectSources is the closed set of connect-src sources a
// worker profile may name. Exactly 'self': the delivery contract is that
// runtimes and models are pinned to the app's own origin, so a worker never
// needs to name another origin — and a profile that tried to would be
// turning this server into an open proxy one URL at a time.
var allowedWorkerConnectSources = map[string]bool{
	"'self'": true,
}

// validateWorkerProfile checks every token against the allowlists.
// [AssetServer.AddBytes] panics on the error, so a typo'd or invented token
// is a boot failure, not a silently unsatisfiable policy.
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

// workerCSP assembles the Content-Security-Policy for a trusted worker's
// script response: a fixed skeleton plus the profile's allowlisted tokens.
//
// The skeleton, and why it differs from both the host document's policy and
// [framedCSP]:
//
//   - default-src 'self': the worker is same-origin and NOT opaque, so
//     'self' is its own origin here — unlike the opaque plugin frame, where
//     'self' means null and spec-correct browsers (Safari) refuse the
//     frame's own subresources, the reason framedCSP keys to the explicit
//     request origin instead.
//   - script-src 'self' plus tokens: same-origin glue (importScripts counts
//     against script-src) and the opted-in compilation keywords, nothing
//     else. No 'unsafe-inline': a worker has no inline markup.
//   - connect-src 'none' unless the profile names 'self': a worker with no
//     fetch need gets no fetch grant; with it, exactly same-origin
//     fetch/XHR/WebSocket for the wasm binary and the model bytes.
//   - worker-src 'self': nested same-origin workers stay possible, and
//     browsers that predate worker-src (older Safari) fall back to
//     script-src, which already carries 'self', so both agree.
//   - object-src 'none': a worker has no plugin fallback to offer.
//
// Like framedCSP this is the AUTHORITATIVE assembly point: tokens are
// re-filtered through the allowlists regardless of the registration-time
// validation, so a profile that somehow skipped [validateWorkerProfile]
// cannot smuggle a keyword or a source into the header. A profile whose
// tokens are all rejected degrades to the strict skeleton, never to an
// open policy.
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

// CacheProfile names the Cache-Control posture for one byte-backed asset.
// An enum, not a string: the exact header text stays the server's decision,
// so a profile cannot smuggle a directive (a hand-written "public" on an
// authenticated page's worker, say). [CacheDefault] keeps the posture every
// asset had before profiles existed.
type CacheProfile int

const (
	// CacheDefault is the pre-profile posture: "no-store, max-age=0". These
	// dev assets carry no cache validator and are referenced by
	// un-versioned paths, so a stale browser copy must not linger across
	// rebuilds. Byte-identical to what every asset served before profiles
	// existed; the default cannot drift.
	CacheDefault CacheProfile = iota

	// CachePublicImmutable is "public, max-age=31536000, immutable": shared
	// caches along the path may store the bytes and never revalidate within
	// a year. Only for URLs whose path changes when the bytes change (a
	// content hash) AND whose bytes are secret-free — public means any
	// intermediary may keep a copy.
	CachePublicImmutable

	// CachePrivateRevalidate is "private, no-cache": the browser may store
	// the asset but MUST revalidate before every reuse. The auth-compatible
	// middle ground — no shared cache ever sees it, and a logged-out or
	// revoked session stops getting fresh copies.
	CachePrivateRevalidate

	// CachePrivateNoStore is "private, no-store": nothing stores it,
	// anywhere, ever. For worker bytes that ship per-session or that must
	// not survive a logout on a shared machine.
	CachePrivateNoStore
)

// cacheControl returns the exact Cache-Control header for the profile. An
// out-of-range value (only constructible by skipping
// [AssetServer.AddBytes]'s validation) degrades to the default no-store
// posture rather than to anything shared caches may keep.
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

// valid reports whether c is one of the defined profiles.
func (c CacheProfile) valid() bool {
	return c >= CacheDefault && c <= CachePrivateNoStore
}

// assetOptions carries what [AssetServer.AddBytes]'s options changed.
type assetOptions struct {
	worker *WorkerCSP
	cache  CacheProfile
}

// AssetOption customises one byte-backed asset registered through
// [AssetServer.AddBytes]. Options are validated there, at registration: an
// invalid combination panics at boot with the cause, the same posture
// [AssetServer.Register] takes on a nil fs.FS.
type AssetOption func(*assetOptions)

// WithWorkerCSP marks the asset as a trusted host-page worker and sets its
// response CSP profile (see [WorkerCSP]). Its presence IS the worker kind:
// framed assets keep the fixed platform policy (passing it together with
// framed=true panics at registration), and a byte asset without it stays a
// plain host script with no policy of its own.
func WithWorkerCSP(p WorkerCSP) AssetOption {
	return func(o *assetOptions) { o.worker = &p }
}

// WithCache sets an explicit cache posture for the asset. Works on any
// byte-backed asset, worker or host script; without it the asset keeps
// [CacheDefault].
func WithCache(c CacheProfile) AssetOption {
	return func(o *assetOptions) { o.cache = c }
}
