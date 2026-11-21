package runtime

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/core-ui/compute"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/runtime/minify"
)

//go:embed frag/*.js
var fragFS embed.FS

//go:embed colorscheme.js
var colorSchemeFS embed.FS

//go:embed src/*.js
var modulesFS embed.FS

var (
	bundleOnce  sync.Once
	bundleData  string
	bundleErr   error
	modulesOnce sync.Once
	modulesData map[string]string
)

const iifeHeader = "// GoFastr Core-UI Runtime v0.4, ES2020+ (composed: full = kernel+signals+nav+widgets-boot)\n" +
	"// Assembled by the Go composer (core-ui/runtime/runtime.go composeFull) from\n" +
	"// core-ui/runtime/frag/*.js; RuntimeJS() serves it minified.\n" +
	"(() => {\n" +
	"  'use strict';\n"

const iifeFooter = "\n})();\n"

var fullFragmentOrder = []string{"kernel", "signals", "nav", "widgets-boot", "boot"}

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

// RuntimeJS returns the full composition, the runtime every page ships.
func RuntimeJS() (string, error) {
	bundleOnce.Do(func() {
		raw, err := composeFull()
		if err != nil {
			bundleErr = err
			return
		}
		bundleData = minify.ServedJS(raw)
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

const staticIifeHeader = "// GoFastr Core-UI Runtime v0.4, ES2020+ (composed: static = kernel+rpc-stub+signals+nav+widgets-boot-static)\n" +
	"// Assembled by the Go composer (core-ui/runtime/runtime.go composeStatic) from\n" +
	"// core-ui/runtime/frag/*.js. Served only by the static exporter\n" +
	"// (framework/static.Builder) for serverless SSG output.\n" +
	"(() => {\n" +
	"  'use strict';\n"

var staticFragmentOrder = []string{"kernel", "rpc-stub", "signals", "nav", "widgets-boot-static", "boot"}

func composeStatic() (string, error) {
	return composeFragments(staticIifeHeader, staticFragmentOrder)
}

var (
	staticOnce sync.Once
	staticData string
	staticErr  error
)

// StaticJS returns the static composition a serverless export ships.
func StaticJS() (string, error) {
	staticOnce.Do(func() {
		raw, err := composeStatic()
		if err != nil {
			staticErr = err
			return
		}
		staticData = minify.ServedJS(raw)
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

const embedIifeHeader = "// GoFastr Core-UI Runtime v0.4, ES2020+ (composed: embed = kernel+signals+widgets-boot+boot-embed)\n" +
	"// Assembled by the Go composer (core-ui/runtime/runtime.go composeEmbed) from\n" +
	"// core-ui/runtime/frag/*.js. Served at /__gofastr/embed-runtime.js and loaded\n" +
	"// only inside an embed frame.\n" +
	"(() => {\n" +
	"  'use strict';\n"

var embedFragmentOrder = []string{"kernel", "signals", "widgets-boot", "boot", "boot-embed"}

func composeEmbed() (string, error) {
	return composeFragments(embedIifeHeader, embedFragmentOrder)
}

var (
	embedOnce sync.Once
	embedData string
	embedErr  error
)

// EmbedJS returns the embed composition served inside an embed frame.
func EmbedJS() (string, error) {
	embedOnce.Do(func() {
		raw, err := composeEmbed()
		if err != nil {
			embedErr = err
			return
		}
		embedData = minify.ServedJS(raw)
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
func EmbedLoaderJS() (string, error) {
	embedLoaderOnce.Do(func() {
		raw, err := fs.ReadFile(embedLoaderFS, "embed-loader.js")
		if err != nil {
			embedLoaderErr = err
			return
		}
		embedLoaderData = minify.ServedJS(string(raw))
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

// ColorSchemeJS returns the first-paint bootstrap that applies the stored
// colour scheme and page theme before the page renders.
func ColorSchemeJS() (string, error) {
	colorSchemeOnce.Do(func() {
		data, err := fs.ReadFile(colorSchemeFS, "colorscheme.js")
		if err != nil {
			colorSchemeErr = err
			return
		}
		colorSchemeSrc = minify.ServedJS(string(data))
	})
	return colorSchemeSrc, colorSchemeErr
}

var (
	colorSchemeOnce sync.Once
	colorSchemeSrc  string
	colorSchemeErr  error
)

// Module returns the served source of an embedded module or registered behaviour.
func Module(name string) (string, bool) {
	if !compute.ValidName(name) {
		return "", false
	}
	if src, ok := embeddedModule(name); ok {
		return src, true
	}
	return registeredModule(name)
}

func loadModules() {
	entries, err := fs.ReadDir(modulesFS, "src")
	if err != nil {
		modulesData = map[string]string{}
		return
	}
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
		modulesData[name] = minify.ServedJS(string(raw))
	}
}

// ModuleSize returns the byte size of a module, or 0.
func ModuleSize(name string) int {
	src, ok := Module(name)
	if !ok {
		return 0
	}
	return len(src)
}

// ModuleNames returns the sorted embedded modules and registered behaviours.
func ModuleNames() []string {
	out := embeddedModuleNames()
	seen := make(map[string]bool, len(out))
	for _, n := range out {
		seen[n] = true
	}
	for _, e := range registry.Behaviors() {
		if seen[e.Name] {
			panic("runtime: behaviour " + e.Name + " shadows an embedded runtime module of the same name")
		}
		out = append(out, e.Name)
	}
	sort.Strings(out)
	return out
}
