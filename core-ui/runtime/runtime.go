// Package runtime ships the framework's client-side JavaScript runtime.
//
// Two surfaces are exposed:
//
//   - The legacy bundled runtime (`runtime.js`) — the one-script payload
//     that handles every framework primitive (dispatchRPC, SPA router,
//     screen cache, signals, widgets, etc). Served at
//     `/__gofastr/runtime.js` for back-compat with pages that haven't
//     adopted the module-loader yet.
//
//   - Per-module bundles (`src/<name>.js`) — small payloads loaded on
//     demand via `__gofastr.loadModule(name)`. Today the canary module
//     is `fileupload`; future phases peel popover, toasts, menu, sse,
//     forms, and widgets out the same way (see
//     docs/runtime-code-split-plan.md).
//
// The HTTP server (core-ui/widget/server.go) consumes Module(name) +
// ModuleNames() to wire `/__gofastr/runtime/<name>.js` routes; the
// uihost emits `<link rel="modulepreload">` tags per page based on the
// components rendered on it.
package runtime

import (
	"embed"
	"io/fs"
	"sort"
	"strings"
)

//go:embed runtime.js
var bundleFS embed.FS

//go:embed src/*.js
var modulesFS embed.FS

// RuntimeJS returns the legacy bundled runtime — the single-file IIFE
// every page used to ship. Kept for back-compat while the module-loader
// migration is in progress.
func RuntimeJS() (string, error) {
	data, err := fs.ReadFile(bundleFS, "runtime.js")
	if err != nil {
		return "", err
	}
	return string(data), nil
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

// Module returns the source of a single split runtime module by name
// (e.g. "fileupload"). Used by the HTTP server to serve
// /__gofastr/runtime/<name>.js. Returns "", false when the module is
// not embedded.
func Module(name string) (string, bool) {
	if !validModuleName(name) {
		return "", false
	}
	data, err := fs.ReadFile(modulesFS, "src/"+name+".js")
	if err != nil {
		return "", false
	}
	return string(data), true
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
	entries, err := fs.ReadDir(modulesFS, "src")
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		n := e.Name()
		if !strings.HasSuffix(n, ".js") {
			continue
		}
		out = append(out, strings.TrimSuffix(n, ".js"))
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
