package store

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	gofastrruntime "github.com/DonaldMurillo/gofastr/core-ui/runtime"
)

func TestPersistStampsMarkerOnEveryBinding(t *testing.T) {
	resetForTest()
	teams := New("teambuilder").String("teams", "")
	teams.Persist()

	want := `data-fui-signal-persist="` + strconv.Itoa(PersistDefaultMaxBytes) + `"`
	cases := map[string]string{
		"Bind":     string(teams.Bind(context.Background(), "span", nil)),
		"BindAttr": string(teams.BindAttr(context.Background(), "a", "title", map[string]string{"href": "/"})),
		"BindHTML": string(teams.BindHTML(context.Background(), "div", nil)),
	}
	for which, html := range cases {
		if !strings.Contains(html, want) {
			t.Errorf("%s did not stamp the persistence marker (%s): %s", which, want, html)
		}
		if !strings.Contains(html, `data-fui-signal="teambuilder.teams"`) {
			t.Errorf("%s: the marker is useless without the name beside it: %s", which, html)
		}
	}
}

// A binding the attribute allow-list refuses renders as static markup,
// and it must not claim to be persisted either: the runtime reads the
// slice name off data-fui-signal, which is not there.
func TestPersistNotStampedOnARefusedAttrBinding(t *testing.T) {
	resetForTest()
	sl := New("app").String("v", "").Persist()
	html := string(sl.BindAttr(context.Background(), "iframe", "srcdoc", nil))
	if strings.Contains(html, "data-fui-signal-persist") {
		t.Fatalf("a refused attr binding stamped the persistence marker with no signal name: %s", html)
	}
}

func TestPersistIsNotStampedWithoutTheOptIn(t *testing.T) {
	resetForTest()
	sl := New("app").String("v", "")
	if sl.Persisted() {
		t.Error("a plain slice reports itself persisted")
	}
	if got := sl.PersistMaxBytes(); got != 0 {
		t.Errorf("PersistMaxBytes on a plain slice = %d, want 0", got)
	}
	if html := string(sl.Bind(context.Background(), "span", nil)); strings.Contains(html, "data-fui-signal-persist") {
		t.Fatalf("a plain slice stamped the persistence marker: %s", html)
	}
}

// Persist implies Global. A page-scoped persisted slice would be
// re-seeded unconditionally by every SPA-nav partial, clobbering the
// browser's value in memory on each navigation; the app-global merge
// rule ("seed a global only the first time it is seen") is what keeps
// it alive.
func TestPersistImpliesGlobal(t *testing.T) {
	resetForTest()
	sl := New("app").String("v", "").Persist()
	if sl.Scope() != ScopeGlobal {
		t.Fatalf("scope = %v, want ScopeGlobal", sl.Scope())
	}
	if ScopeOf("app.v") != ScopeGlobal {
		t.Fatal("the declaration registry did not record the global scope")
	}
}

func TestPersistMaxCarriesTheDeclaredCap(t *testing.T) {
	resetForTest()
	sl := New("app").String("v", "").PersistMax(4096)
	if got := sl.PersistMaxBytes(); got != 4096 {
		t.Fatalf("PersistMaxBytes = %d, want 4096", got)
	}
	if html := string(sl.Bind(context.Background(), "span", nil)); !strings.Contains(html, `data-fui-signal-persist="4096"`) {
		t.Fatalf("the declared cap did not reach the markup: %s", html)
	}
}

func TestPersistMaxRefusesCapsOutsideTheRange(t *testing.T) {
	for _, n := range []int{0, -1, PersistMaxBytesLimit + 1} {
		t.Run(strconv.Itoa(n), func(t *testing.T) {
			resetForTest()
			defer func() {
				if recover() == nil {
					t.Fatalf("PersistMax(%d) did not panic", n)
				}
			}()
			New("app").String("v", "").PersistMax(n)
		})
	}
}

func TestPersistMaxAcceptsTheRangeEdges(t *testing.T) {
	resetForTest()
	if got := New("a").String("v", "").PersistMax(1).PersistMaxBytes(); got != 1 {
		t.Errorf("PersistMax(1) = %d", got)
	}
	resetForTest()
	if got := New("b").String("v", "").PersistMax(PersistMaxBytesLimit).PersistMaxBytes(); got != PersistMaxBytesLimit {
		t.Errorf("PersistMax(limit) = %d", got)
	}
}

// A computed slice is recomputed from its dependencies on every load,
// so a persisted copy would restore a stale answer the reducer then
// overwrites. Refused where it is written.
func TestPersistRefusesAComputedSlice(t *testing.T) {
	resetForTest()
	_ = New("org").String("companyName", "Acme")
	greeting := Computed[string](New("org"), "greeting", "greet", "org.companyName")
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("persisting a computed slice did not panic")
		}
		if !strings.Contains(strings.ToLower(toString(r)), "computed") {
			t.Fatalf("the panic does not name the reason: %v", r)
		}
	}()
	greeting.Persist()
}

// The runtime half must be registered under the name and marker the
// docs and the attribute table promise, or the kernel never loads it.
func TestPersistBehaviorIsRegistered(t *testing.T) {
	e, ok := registry.LookupBehavior("signal-persist")
	if !ok {
		t.Fatal("signal-persist is not registered — a persisted slice would render a marker nothing reads")
	}
	if len(e.Markers) != 1 || e.Markers[0] != "[data-fui-signal-persist]" {
		t.Fatalf("markers = %v, want exactly [data-fui-signal-persist]", e.Markers)
	}
	if e.Idle {
		t.Error("the module is idle-loaded: the browser's value would arrive later than it has to")
	}
	// The browser store itself is the kernel's primitive; this module is
	// the join to the signal bus and must declare the dependency, or it
	// evaluates against an absent window.__gofastr.local.
	if len(e.Requires) != 1 || e.Requires[0] != "local" {
		t.Fatalf("requires = %v, want exactly [local]", e.Requires)
	}
	// The engine is the primitive's business. A call to either Web
	// storage API here would mean the layering had leaked back.
	for _, sink := range []string{"localStorage.", "sessionStorage.", "window.indexedDB"} {
		if strings.Contains(e.Source, sink) {
			t.Errorf("the signal-persist module calls %s directly — the storage layer is the local primitive's, not this module's", sink)
		}
	}
}

// The namespace the docs and the attribute table promise is the one the
// primitive writes. It lives in core-ui/runtime/src/local.js
// now, so this is the gate that keeps the two documents honest.
func TestLocalPrimitiveCarriesTheDocumentedNamespace(t *testing.T) {
	src, ok := gofastrruntime.Module("local")
	if !ok {
		t.Fatal("the local primitive is not an embedded runtime module — signal-persist would require a module that does not exist")
	}
	if !strings.Contains(src, "gofastr.state.") {
		t.Error("the local primitive does not carry the documented storage namespace")
	}
	if !strings.Contains(src, "indexedDB") {
		t.Error("the local primitive does not reach IndexedDB — localStorage is the fallback, not the engine")
	}
}

// The default cap the Go side stamps and the fallback the module uses
// when the attribute is unreadable must be the same number, or a
// binding rendered by an older server disagrees with the module.
func TestPersistDefaultCapMatchesTheModuleFallback(t *testing.T) {
	want := "const DEFAULT_MAX_BYTES = " + strconv.Itoa(PersistDefaultMaxBytes) + ";"
	if !strings.Contains(persistJS, want) {
		t.Fatalf("persist.js does not carry %q — the Go default and the JS fallback have drifted", want)
	}
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if e, ok := v.(error); ok {
		return e.Error()
	}
	return ""
}

// Every reason persist.js can put on gofastr:persist-overflow is one the
// docs enumerate. The module grew a fifth value (untrusted) while the
// three documents still listed four, so the list is read from the source
// and checked against each document's enumeration rather than
// remembered. The enumeration is parsed, not searched: a reason named
// in prose beside the list does not count as listed.
func TestPersistOverflowReasonsAreDocumented(t *testing.T) {
	reasons := regexp.MustCompile(`notify\(name, '([a-z]+)'`).FindAllStringSubmatch(persistJS, -1)
	if len(reasons) < 2 {
		t.Fatalf("persist.js spells %d literal reasons: the scan is broken, not the module clean", len(reasons))
	}
	// The primitive's own reasons pass through r.reason; they are the
	// set local.js documents on set().
	names := map[string]bool{"quota": true, "encode": true, "unavailable": true}
	for _, m := range reasons {
		names[m[1]] = true
	}
	// The two spellings the docs use: the attribute table's
	// reason: "a"|"b" and the guide's `reason` one of `a`, `b`.
	enums := []*regexp.Regexp{
		regexp.MustCompile(`reason: ((?:"[a-z]+"\|?)+)`),
		regexp.MustCompile("`reason` one of ((?:`[a-z]+`,?\\s*)+)"),
	}
	word := regexp.MustCompile(`[a-z]+`)
	docs := []string{
		filepath.Join("..", "..", "framework", "docs", "content", "runtime-contract.md"),
		filepath.Join("..", "..", "framework", "docs", "content", "signal-store.md"),
		filepath.Join("..", "ARCHITECTURE.md"),
	}
	for _, p := range docs {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		listed := map[string]bool{}
		for _, re := range enums {
			for _, m := range re.FindAllStringSubmatch(string(raw), -1) {
				for _, w := range word.FindAllString(m[1], -1) {
					listed[w] = true
				}
			}
		}
		if len(listed) == 0 {
			t.Errorf("%s: no persist-overflow reason enumeration found", p)
			continue
		}
		for name := range names {
			if !listed[name] {
				t.Errorf("%s does not list the persist-overflow reason %q (lists %v)", p, name, listed)
			}
		}
	}
}
