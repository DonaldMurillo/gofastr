// Package runtime ships the framework's client-side JavaScript runtime.
//
// Two surfaces are exposed:
//
//   - The bundled runtime (`runtime.js`) — the one-script payload that
//     handles every framework primitive (dispatchRPC, SPA router,
//     screen cache, signals, widgets, etc). Served at
//     `/__gofastr/runtime.js`. This is the default surface.
//
//   - Per-module bundles (`src/<name>.js`) — small payloads loaded on
//     demand via `__gofastr.loadModule(name)`. Used for the optional
//     code-splitting path; pages opt into individual modules instead
//     of the bundled runtime when bundle size matters.
//
// The HTTP server (core-ui/widget/server.go) consumes Module(name) +
// ModuleNames() to wire `/__gofastr/runtime/<name>.js` routes; the
// uihost emits `<link rel="modulepreload">` tags per page based on the
// components rendered on it.
package runtime

import (
	"embed"
	"io/fs"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/core-ui/runtime/minify"
)

//go:embed runtime.js
var bundleFS embed.FS

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

// RuntimeJS returns the bundled runtime — the single-file IIFE every
// page ships by default. Minified on first call (or returned verbatim
// when RUNTIME_NOMINIFY=1).
func RuntimeJS() (string, error) {
	bundleOnce.Do(func() {
		raw, err := fs.ReadFile(bundleFS, "runtime.js")
		if err != nil {
			bundleErr = err
			return
		}
		if nominify() {
			bundleData = string(raw)
			return
		}
		bundleData = minify.Minify(string(raw))
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
