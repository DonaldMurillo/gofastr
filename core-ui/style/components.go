package style

import (
	"fmt"
	"maps"
	"regexp"
	"sort"
	"sync"
)

var componentKeyRe = regexp.MustCompile(`^[a-z][a-z0-9-]*(\.[a-z][a-z0-9-]*)*$`)

var componentValueRe = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

var customPropNameRe = regexp.MustCompile(`^--[a-zA-Z][a-zA-Z0-9-]*$`)

func validateComponents(m map[string]string) (err error) {
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
	componentCompiler.mu.Lock()
	fn := componentCompiler.fn
	componentCompiler.mu.Unlock()
	if fn == nil {
		return nil
	}
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("theme: Components: %v", p)
		}
	}()
	fn(m)
	return nil
}

func validateComponentEntry(key, value string) error {
	if !componentKeyRe.MatchString(key) {
		return fmt.Errorf("theme: Components key %q: want lowercase dot-separated words (\"density\", \"button.treatment\")", key)
	}
	if !componentValueRe.MatchString(value) {
		return fmt.Errorf("theme: Components[%q]: value %q: want one lowercase word (\"compact\", \"outline\")", key, value)
	}
	return nil
}

// Declaration is one custom property a component-options compiler returns for
// a theme's Components.
type Declaration struct {
	Name  string
	Value string
}

var componentCompiler struct {
	mu       sync.Mutex
	fn       func(components map[string]string) []Declaration
	defaults map[string]string
	frozen   bool
}

// RegisterComponentOptionsCompiler installs the one function that turns a
// theme's flattened Components into custom-property declarations, together
// with the styled layer's complete default option set.
func RegisterComponentOptionsCompiler(fn func(components map[string]string) []Declaration, defaults map[string]string) {
	if fn == nil {
		panic("style: RegisterComponentOptionsCompiler needs a compiler, not nil")
	}
	if len(defaults) == 0 {
		panic("style: RegisterComponentOptionsCompiler needs the styled layer's complete default option set — " +
			"an optionless theme would otherwise emit no option variables and the component rules would resolve to nothing")
	}
	for _, k := range sortedMapKeys(defaults) {
		if err := validateComponentEntry(k, defaults[k]); err != nil {
			panic(fmt.Sprintf("style: RegisterComponentOptionsCompiler: default option %q: %v", k, err))
		}
	}
	validatedDeclLines(fn(defaults))
	componentCompiler.mu.Lock()
	defer componentCompiler.mu.Unlock()
	if componentCompiler.frozen {
		panic("style: RegisterComponentOptionsCompiler called after theme CSS emission: " +
			"register the compiler before calling CSSCustomProperties or ThemeOverrideCSS")
	}
	if componentCompiler.fn != nil {
		panic("style: a component-options compiler is already registered: one per process (framework/ui registers it from its init)")
	}
	componentCompiler.fn = fn
	componentCompiler.defaults = maps.Clone(defaults)
}

func withDefaultOptions(own map[string]string) map[string]string {
	defaults := componentCompilerDefaults()
	if len(defaults) == 0 {
		return own
	}
	merged := maps.Clone(defaults)
	maps.Copy(merged, own)
	return merged
}

func componentOptionDecls(components map[string]string) []string {
	componentCompiler.mu.Lock()
	fn := componentCompiler.fn
	componentCompiler.frozen = true
	componentCompiler.mu.Unlock()
	if fn == nil || len(components) == 0 {
		return nil
	}
	return validatedDeclLines(fn(components))
}

func validatedDeclLines(decls []Declaration) []string {
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

func resetComponentOptionsForTest() {
	componentCompiler.mu.Lock()
	componentCompiler.fn = nil
	componentCompiler.defaults = nil
	componentCompiler.frozen = false
	componentCompiler.mu.Unlock()
}

func componentOptionsFrozenForTest() bool {
	componentCompiler.mu.Lock()
	defer componentCompiler.mu.Unlock()
	return componentCompiler.frozen
}
