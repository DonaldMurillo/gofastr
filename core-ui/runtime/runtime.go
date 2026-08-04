// Package runtime ships the framework's client-side JavaScript runtime.
//
// Two surfaces are exposed:
//
//   - The core runtime (`runtime.js`) — the one-script substrate for SPA
//     navigation, signals, widgets, hydration, and demand-module loading.
//     Served at `/__gofastr/runtime.js`.
//
//   - Per-module bundles (`src/<name>.js`) — payloads loaded on demand via
//     `__gofastr.loadModule(name)`. RPC is prefetched when its marker exists
//     and awaited by the core delegation bridge; optional UI behaviors use
//     the same loader.
//
// The HTTP server (core-ui/widget/server.go) consumes Module(name) +
// ModuleNames() to wire `/__gofastr/runtime/<name>.js` routes; the
// uihost emits `<link rel="preload" as="script">` tags per page based on the
// components rendered on it.
package runtime

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/core-ui/runtime/minify"
)

//go:embed frag/*.js
var fragFS embed.FS

//go:embed colorscheme.js
var colorSchemeFS embed.FS

//go:embed src/*.js
var modulesFS embed.FS

// nominify reports whether minification should be skipped on this
// process. The default is "production wins":
//
//   - GOFASTR_ENV set to "production"/"prod"/"live"/"staging" → minify.
//   - GOFASTR_DEV truthy (and GOFASTR_ENV not a non-dev env) → keep raw
//     so browser devtools show readable stack traces.
//   - Neither set → minify (the right default for any app that just
//     `go run`s its binary in production with no env hints).
//   - RUNTIME_NOMINIFY truthy → force raw (manual override; trumps the
//     env detection so a dev can debug a production-config app).
//   - RUNTIME_MINIFY truthy → force minify (manual override; useful when
//     reproducing a prod issue from a dev workstation).
//
// Evaluated once at startup; flipping the env mid-process has no effect
// because Module/RuntimeJS results are cached behind sync.Once.
var nominifyOnce sync.Once
var nominifyVal bool

func nominify() bool {
	nominifyOnce.Do(func() {
		// Explicit manual overrides win.
		if envBool("RUNTIME_NOMINIFY") {
			nominifyVal = true
			return
		}
		if envBool("RUNTIME_MINIFY") {
			nominifyVal = false
			return
		}
		// Production-shaped env → minify.
		if isNonDevEnv(os.Getenv("GOFASTR_ENV")) {
			nominifyVal = false
			return
		}
		// Explicit dev-mode → skip minify.
		if envBool("GOFASTR_DEV") {
			nominifyVal = true
			return
		}
		// Nothing set → minify by default.
		nominifyVal = false
	})
	return nominifyVal
}

func envBool(key string) bool {
	v := os.Getenv(key)
	if v == "" {
		return false
	}
	b, err := strconv.ParseBool(v)
	return err == nil && b
}

func isNonDevEnv(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "production", "prod", "live", "staging":
		return true
	}
	return false
}

// Cached minified payloads. Computed lazily on first read so the
// minify pass runs at most once per source per process; subsequent
// reads (and there are many — every page render) are pure map lookups.
var (
	bundleOnce  sync.Once
	bundleData  string
	bundleErr   error
	modulesOnce sync.Once
	modulesData map[string]string
)

// composeFull assembles the `full` core runtime: kernel + signals + nav +
// widgets-boot plus boot.js (the kernel boot tail), concatenated inside one
// IIFE. RPC request machinery is a demand module; boot retains its delegated
// interaction bridge.
//
// The served runtime.js is assembled from fragments so static and embed
// compositions can select their boot behavior without copying code.
//
// Composed once at init and cached (bundleOnce in RuntimeJS); the
// token-aware minifier runs on the composed output, unchanged.
const iifeHeader = "// GoFastr Core-UI Runtime v0.4 — ES2020+ (composed: full = kernel+signals+nav+widgets-boot)\n" +
	"// Assembled by the Go composer (core-ui/runtime/runtime.go composeFull) from\n" +
	"// core-ui/runtime/frag/*.js. This file is the on-disk canonical form the gate\n" +
	"// tests scan (attrdoc_test.go / integrity_test.go read it via os.ReadFile);\n" +
	"// RuntimeJS() serves the minified composition produced by composeFull().\n" +
	"(() => {\n" +
	"  'use strict';\n"

const iifeFooter = "\n})();\n"

// fullFragmentOrder is the composition order for the `full` bundle.
// kernel creates window.__gofastr; boot runs last because its initial pass
// calls nav and signal helpers. The RPC implementation is src/rpc.js and is
// loaded through boot's delegated interaction bridge.
var fullFragmentOrder = []string{"kernel", "signals", "nav", "widgets-boot", "boot"}

// composeFragments concatenates the named fragments inside one IIFE. Every
// composition goes through it, so a new bundle cannot accidentally assemble
// itself differently from the ones the gate tests cover.
func composeFragments(header string, order []string) (string, error) {
	var b strings.Builder
	b.WriteString(header)
	for _, name := range order {
		data, err := fs.ReadFile(fragFS, "frag/"+name+".js")
		if err != nil {
			return "", fmt.Errorf("compose fragment %q: %w", name, err)
		}
		b.WriteByte('\n')
		b.WriteString(strings.TrimRight(string(data), "\n"))
		b.WriteByte('\n')
	}
	b.WriteString(iifeFooter)
	return b.String(), nil
}

func composeFull() (string, error) {
	return composeFragments(iifeHeader, fullFragmentOrder)
}

// RuntimeJS returns the composed runtime — the single-file IIFE every
// page ships by default. Assembled from the fragment files once, then
// minified (or returned verbatim when RUNTIME_NOMINIFY=1).
func RuntimeJS() (string, error) {
	bundleOnce.Do(func() {
		raw, err := composeFull()
		if err != nil {
			bundleErr = err
			return
		}
		if nominify() {
			bundleData = raw
			return
		}
		bundleData = minify.Minify(raw)
	})
	return bundleData, bundleErr
}

// MustRuntimeJS returns the bundled runtime or panics.
func MustRuntimeJS() string {
	js, err := RuntimeJS()
	if err != nil {
		panic(err)
	}
	return js
}

// RuntimeSize returns the byte size of the bundled runtime.
func RuntimeSize() int {
	js, err := RuntimeJS()
	if err != nil {
		return 0
	}
	return len(js)
}

// staticIifeHeader is the IIFE wrapper for the `static` composition (SSG
// export). Same wrapper structure as iifeHeader, but names the static
// fragment set and omits the on-disk-canonical-form note — only `full`
// (runtime.js) has an on-disk form the gate tests scan. The static bundle
// is assembled purely in memory and served by StaticJS().
const staticIifeHeader = "// GoFastr Core-UI Runtime v0.4 — ES2020+ (composed: static = kernel+rpc-stub+signals+nav+widgets-boot-static)\n" +
	"// Assembled by the Go composer (core-ui/runtime/runtime.go composeStatic) from\n" +
	"// core-ui/runtime/frag/*.js. Served only by the static exporter\n" +
	"// (framework/static.Builder) for serverless SSG output.\n" +
	"(() => {\n" +
	"  'use strict';\n"

// staticFragmentOrder is the composition order for the `static` bundle.
// rpc-stub runs before the shared boot tail, installs the serverless notice,
// and prevents boot from installing or prefetching the live RPC bridge.
// widgets-boot-static fetches the exported widget catalog and chrome while nav
// continues to work against static HTML.
var staticFragmentOrder = []string{"kernel", "rpc-stub", "signals", "nav", "widgets-boot-static", "boot"}

// composeStatic assembles the `static` runtime composition for SSG export.
// Same concatenation shape as composeFull — fragments are embedded JS files
// joined inside one IIFE so closure semantics are preserved exactly.
func composeStatic() (string, error) {
	return composeFragments(staticIifeHeader, staticFragmentOrder)
}

var (
	staticOnce sync.Once
	staticData string
	staticErr  error
)

// StaticJS returns the `static` runtime composition used by serverless exports.
// rpc-stub intercepts server-backed controls and boot skips the RPC module
// marker. widgets-boot-static reads the dumped widget catalog so overlays still
// resolve from exported files.
func StaticJS() (string, error) {
	staticOnce.Do(func() {
		raw, err := composeStatic()
		if err != nil {
			staticErr = err
			return
		}
		if nominify() {
			staticData = raw
			return
		}
		staticData = minify.Minify(raw)
	})
	return staticData, staticErr
}

// MustStaticJS returns the static runtime or panics.
func MustStaticJS() string {
	js, err := StaticJS()
	if err != nil {
		panic(err)
	}
	return js
}

// embedIifeHeader is the IIFE wrapper for the `embed` composition — the
// runtime that ships INSIDE an embedded surface's iframe.
const embedIifeHeader = "// GoFastr Core-UI Runtime v0.4 — ES2020+ (composed: embed = kernel+signals+widgets-boot+boot-embed)\n" +
	"// Assembled by the Go composer (core-ui/runtime/runtime.go composeEmbed) from\n" +
	"// core-ui/runtime/frag/*.js. Served at /__gofastr/embed-runtime.js and loaded\n" +
	"// only inside an embed frame.\n" +
	"(() => {\n" +
	"  'use strict';\n"

// embedFragmentOrder is the composition order for the `embed` bundle.
//
// The nav fragment is ABSENT, and that absence is the feature: SPA navigation
// inside a frame is impossible because the code is not there, not because a
// flag is off. Nothing downstream can re-enable it by mistake.
//
// boot still ships for hydration, mutation observation, and demand loading.
// Its updateActiveLink call is guarded because nav is absent. boot-embed runs
// last after the core namespace is complete.
var embedFragmentOrder = []string{"kernel", "signals", "widgets-boot", "boot", "boot-embed"}

func composeEmbed() (string, error) {
	return composeFragments(embedIifeHeader, embedFragmentOrder)
}

var (
	embedOnce sync.Once
	embedData string
	embedErr  error
)

// EmbedJS returns the `embed` runtime composition — the bundle served inside an
// embedded surface's iframe.
//
// Its budget is looser than the core runtime's on purpose: it loads inside a
// frame and blocks nothing on the host page. It is still budgeted, because how
// fast an embed paints is the product.
func EmbedJS() (string, error) {
	embedOnce.Do(func() {
		raw, err := composeEmbed()
		if err != nil {
			embedErr = err
			return
		}
		if nominify() {
			embedData = raw
			return
		}
		embedData = minify.Minify(raw)
	})
	return embedData, embedErr
}

// MustEmbedJS returns the embed runtime or panics.
func MustEmbedJS() string {
	js, err := EmbedJS()
	if err != nil {
		panic(err)
	}
	return js
}

//go:embed embed-loader.js
var embedLoaderFS embed.FS

var (
	embedLoaderOnce sync.Once
	embedLoaderData string
	embedLoaderErr  error
)

// EmbedLoaderJS returns the loader a customer pastes into their own page.
//
// This is the tightest budget in the repo: it lands on a stranger's critical
// path, on a site whose performance we do not control and whose owner did not
// choose GoFastr. It creates the iframe, hands over the nonce by postMessage,
// and resizes. Anything else belongs inside the frame.
func EmbedLoaderJS() (string, error) {
	embedLoaderOnce.Do(func() {
		raw, err := fs.ReadFile(embedLoaderFS, "embed-loader.js")
		if err != nil {
			embedLoaderErr = err
			return
		}
		if nominify() {
			embedLoaderData = string(raw)
			return
		}
		embedLoaderData = minify.Minify(string(raw))
	})
	return embedLoaderData, embedLoaderErr
}

// MustEmbedLoaderJS returns the embed loader or panics.
func MustEmbedLoaderJS() string {
	js, err := EmbedLoaderJS()
	if err != nil {
		panic(err)
	}
	return js
}

// ColorSchemeJS returns the color-scheme bootstrap script — a tiny
// synchronous snippet meant to ship at the TOP of <head> so dark-mode
// CSS tokens take effect during the same first paint that hits the
// page. Reads localStorage("gofastr.colorScheme") + the OS
// prefers-color-scheme hint, then sets <html data-color-scheme="…">
// and a matching <meta name="color-scheme">.
//
// Apps that ship a theme toggle call
// `window.__gofastr_colorScheme.set('auto'|'light'|'dark')` to
// override the OS preference.
func ColorSchemeJS() (string, error) {
	data, err := fs.ReadFile(colorSchemeFS, "colorscheme.js")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// Module returns the source of a single split runtime module by name
// (e.g. "fileupload"). Used by the HTTP server to serve
// /__gofastr/runtime/<name>.js. Returns "", false when the module is
// not embedded. Minified on first read (cached).
func Module(name string) (string, bool) {
	if !validModuleName(name) {
		return "", false
	}
	modulesOnce.Do(loadModules)
	src, ok := modulesData[name]
	return src, ok
}

func loadModules() {
	entries, err := fs.ReadDir(modulesFS, "src")
	if err != nil {
		modulesData = map[string]string{}
		return
	}
	skip := nominify()
	modulesData = make(map[string]string, len(entries))
	for _, e := range entries {
		n := e.Name()
		if !strings.HasSuffix(n, ".js") {
			continue
		}
		raw, err := fs.ReadFile(modulesFS, "src/"+n)
		if err != nil {
			continue
		}
		name := strings.TrimSuffix(n, ".js")
		if skip {
			modulesData[name] = string(raw)
		} else {
			modulesData[name] = minify.Minify(string(raw))
		}
	}
}

// ModuleSize returns the byte size of a single embedded module, or 0
// if the module isn't present. Used by tests asserting per-module size
// budgets.
func ModuleSize(name string) int {
	src, ok := Module(name)
	if !ok {
		return 0
	}
	return len(src)
}

// ModuleNames returns the sorted list of split modules currently
// embedded. Each name maps 1:1 to a /__gofastr/runtime/<name>.js URL.
func ModuleNames() []string {
	modulesOnce.Do(loadModules)
	out := make([]string, 0, len(modulesData))
	for name := range modulesData {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// validModuleName rejects path-traversal / weird characters. Keeps
// the file-name-as-URL contract honest.
func validModuleName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '-' || c == '_':
		default:
			return false
		}
	}
	return true
}
