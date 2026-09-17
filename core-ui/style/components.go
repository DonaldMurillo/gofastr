package style

import (
	"fmt"
	"regexp"
	"sort"
	"sync"
)

// Theme.Components: the canonical flattened component options, and the
// compiler hook that turns them into CSS. The field lives on Theme
// (theme.go); this file owns its grammar, its security boundary and the
// one-per-process compiler framework/ui registers.

// componentKeyRe is the grammar of a Components key: lowercase segments
// separated by single dots, each starting with a letter
// ("density", "button.treatment"). Dots namespace a component family;
// the flat form is what ThemeToTokens, ApplyTokens and the theme-edit
// writeback carry, so the grammar is the one contract all of them
// enforce.
var componentKeyRe = regexp.MustCompile(`^[a-z][a-z0-9-]*(\.[a-z][a-z0-9-]*)*$`)

// componentValueRe is the grammar of a Components value: one lowercase
// word ("compact", "outline"). Values are enum names, not CSS; the CSS
// a theme actually ships is the compiler's output, which has its own
// value grammar below.
var componentValueRe = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// customPropNameRe is the grammar of a custom property name a
// component-options compiler may return: "--" then a name of letters,
// digits and dashes. No dots, no spaces, no escapes; a name outside it
// cannot be a selector or anything but a property.
var customPropNameRe = regexp.MustCompile(`^--[a-zA-Z][a-zA-Z0-9-]*$`)

// validateComponents checks every entry of a Components map. Keys are
// visited in sorted order so the first error is deterministic, the same
// posture Theme.Validate takes elsewhere.
func validateComponents(m map[string]string) error {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if err := validateComponentEntry(k, m[k]); err != nil {
			return err
		}
	}
	return nil
}

// validateComponentEntry is the single grammar check a flattened
// component option passes: it is shared by Theme.Validate (a theme
// someone hands the host), ApplyTokens (a value that arrives as text)
// and nothing else, so the three cannot disagree about what a key or a
// value may contain.
func validateComponentEntry(key, value string) error {
	if !componentKeyRe.MatchString(key) {
		return fmt.Errorf("theme: Components key %q: want lowercase dot-separated words (\"density\", \"button.treatment\")", key)
	}
	if !componentValueRe.MatchString(value) {
		return fmt.Errorf("theme: Components[%q]: value %q: want one lowercase word (\"compact\", \"outline\")", key, value)
	}
	return nil
}

// Declaration is one custom property a component-options compiler
// returns for a theme's Components: Name includes the leading "--"
// ("--hui-button-radius"), Value is the exact text after the colon
// ("var(--radii-md)", "0", "9999px").
type Declaration struct {
	Name  string
	Value string
}

// componentCompiler is the process-wide registration state of the
// component-options compiler. One compiler per process, registered from
// a package init, read (and frozen) at the first theme-CSS emission.
var componentCompiler struct {
	mu     sync.Mutex
	fn     func(components map[string]string) []Declaration
	frozen bool
}

// RegisterComponentOptionsCompiler installs the one function that turns
// a theme's flattened Components into custom-property declarations.
// core-ui/style stores, hashes and copies the options but cannot turn
// them into CSS without knowing what they mean; framework/ui knows and
// registers its compiler from its package init. The declarations reach
// CSSCustomProperties (:root) and ThemeOverrideCSS (every scope block),
// which puts them in the app.css identity the host's theme variants
// hash.
//
// It panics when called twice (one compiler per process; the second
// registration is a wiring bug, not a preference) and when called after
// the first theme CSS was emitted: the uihost freezes app.css, the
// component catalog and the manifest in sync.Once at first use, so a
// compiler registered late would be missing from all three and a page
// would render options another page never saw. Register from init,
// before the host serves.
func RegisterComponentOptionsCompiler(fn func(components map[string]string) []Declaration) {
	if fn == nil {
		panic("style: RegisterComponentOptionsCompiler needs a compiler, not nil")
	}
	componentCompiler.mu.Lock()
	defer componentCompiler.mu.Unlock()
	if componentCompiler.frozen {
		panic("style: RegisterComponentOptionsCompiler called after the first theme CSS was emitted: " +
			"the host freezes app.css, the component catalog and the manifest at first use, so a compiler " +
			"registered now would be missing from all three; register from package init, before Mount")
	}
	if componentCompiler.fn != nil {
		panic("style: a component-options compiler is already registered: one per process (framework/ui registers it from its init)")
	}
	componentCompiler.fn = fn
}

// componentOptionDecls compiles one theme's Components into sorted
// "--name: value;" declaration lines, or nil when there is nothing to
// emit. This is the READ side of the hook and the freeze point: the
// first call from either emitter (CSSCustomProperties, ThemeOverrideCSS)
// latches the registration shut; a later RegisterComponentOptionsCompiler
// panics with the reason.
//
// The compiler's output is validated here, at emit, because it reaches
// CSS: a name must be a custom property name and a value must carry no
// declaration-breaking sequence (the same findDeclBreaker set every
// other CSS-bound value answers to — parentheses, dashes, commas and
// spaces are allowed, "var(--spacing-md)" is the expected shape).
// Values are otherwise free-form on purpose: an option's value is where
// token references live, and the compiler is the component family's own
// code.
func componentOptionDecls(components map[string]string) []string {
	componentCompiler.mu.Lock()
	fn := componentCompiler.fn
	componentCompiler.frozen = true
	componentCompiler.mu.Unlock()
	if fn == nil || len(components) == 0 {
		return nil
	}
	decls := fn(components)
	lines := make([]string, 0, len(decls))
	for _, d := range decls {
		if !customPropNameRe.MatchString(d.Name) {
			panic(fmt.Sprintf("style: component-options compiler returned name %q: not a custom property name (--name, letters/digits/dashes)", d.Name))
		}
		if d.Value == "" {
			panic(fmt.Sprintf("style: component-options compiler returned an empty value for %s", d.Name))
		}
		if br := findDeclBreaker(d.Value); br != "" {
			panic(fmt.Sprintf("style: component-options compiler returned value %q for %s: %q breaks the declaration", d.Value, d.Name, br))
		}
		lines = append(lines, d.Name+": "+d.Value+";")
	}
	sort.Strings(lines)
	return lines
}

// resetComponentOptionsForTest restores the compiler hook to its
// never-registered state. Unexported on purpose: the one-per-process
// freeze is a production invariant, and only this package's tests need
// to stage it per test.
func resetComponentOptionsForTest() {
	componentCompiler.mu.Lock()
	componentCompiler.fn = nil
	componentCompiler.frozen = false
	componentCompiler.mu.Unlock()
}
