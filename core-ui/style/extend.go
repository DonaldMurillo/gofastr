package style

import (
	"fmt"
	"maps"
	"reflect"
	"regexp"
	"slices"
	"strings"
)

// Extend returns a copy of t that also carries the app's own tokens.
// Each argument is a struct (or a pointer to one) whose fields are typed
// tokens, nested token structs included:
//
//	type brandTokens struct {
//		BrandGlow style.Color      // --color-brand-glow
//		HeroGap   style.Size       // --size-hero-gap
//		Display   style.FontWeight // --font-weight-display
//	}
//
//	site.WithTheme(theme.Default().Extend(brandTokens{...}))
//
// The value's type picks the prefix, the field name the rest (an
// explicit Name wins), so app tokens share one vocabulary with the
// built-ins. They are emitted on :root and in every themed scope,
// re-declared in the dark blocks when DarkColors names a colour,
// reachable through ThemeToTokens/ApplyTokens (and so the theme
// editor), and part of ThemeHash.
//
// Extend copies its inputs, so changing a struct afterwards does not
// change the theme, and it never modifies t. It panics on input that
// is not a struct of tokens, and when a token would emit a key the
// theme already emits, naming both fields: two tokens on one variable
// would leave the page painted by whichever the cascade reached last.
func (t Theme) Extend(tokens ...any) Theme {
	out := t
	out.Extensions = cloneExtensions(t.Extensions)
	seen := map[string]string{}
	var existing []tokenAt
	walkTokenPaths(reflect.ValueOf(out), "Theme", &existing)
	for _, e := range existing {
		if _, dup := seen[e.Key]; !dup {
			seen[e.Key] = e.Path
		}
	}
	for _, in := range tokens {
		p := copyTokenStruct(in)
		autofillTokens(p.Elem(), nil)
		var added []tokenAt
		walkTokenPaths(p, extensionPath(p), &added)
		if len(added) == 0 {
			panic(fmt.Sprintf("style: Theme.Extend: %s has no typed token fields (style.Color, style.Size, …)", p.Elem().Type()))
		}
		for _, a := range added {
			if prev, ok := seen[a.Key]; ok {
				panic(fmt.Sprintf("style: Theme.Extend: %s and %s both emit --%s; rename one", prev, a.Path, a.Key))
			}
			seen[a.Key] = a.Path
		}
		out.Extensions = append(out.Extensions, p.Interface())
		if d, ok := p.Interface().(DarkTokens); ok {
			out.DarkColors = mergeDarkTokens(out.DarkColors, d.DarkTokens(), added, extensionPath(p))
		}
	}
	return out
}

// DarkTokens is a token set that carries dark-scheme values for its own
// colours, keyed by colour name ("brand-glow" for --color-brand-glow).
// The typed Go `gofastr gen styles` writes from a <name>.tokens.css
// implements it with the file's @media (--dark) block. Extend merges
// the values into Theme.DarkColors when the theme has a dark palette;
// a light-only theme (empty DarkColors) stays light-only, since one
// dark colour among light ones is worse than none.
type DarkTokens interface {
	DarkTokens() map[string]string
}

// mergeDarkTokens returns dark with the set's dark values added, in a
// new map. A key that is not one of the set's own colours, or a value
// outside the colour grammar, panics: the set is compiled in, so the
// mistake is the build's.
func mergeDarkTokens(dark, values map[string]string, own []tokenAt, owner string) map[string]string {
	if len(dark) == 0 || len(values) == 0 {
		return dark
	}
	colors := map[string]bool{}
	for _, a := range own {
		if name, ok := strings.CutPrefix(a.Key, "color-"); ok {
			colors[name] = true
		}
	}
	out := make(map[string]string, len(dark)+len(values))
	maps.Copy(out, dark)
	for _, name := range slices.Sorted(maps.Keys(values)) {
		if !colors[name] {
			panic(fmt.Sprintf("style: Theme.Extend: %s has a dark value for %q, which is not one of its colours", owner, name))
		}
		if err := validateColorValue(values[name]); err != nil {
			panic(fmt.Sprintf("style: Theme.Extend: %s: dark value for %q: %v", owner, name, err))
		}
		out[name] = values[name]
	}
	return out
}

// tokenNamePattern is the name an app token may carry after its type
// prefix: lower-case words joined by single dashes, starting with a
// letter, so it maps to one Go field name and back.
var tokenNamePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

// ParseToken turns one custom-property key and CSS value into the
// typed token it declares: ("size-page-width", "67.5rem") is
// Size{Name: "page-width", Value: "67.5rem"}. The key's prefix picks
// the type (TokenCategory, longest first), and the value must pass the
// check ApplyTokens runs for that type, so a value a generated token
// set compiles in is one the theme editor would also accept.
//
// Breakpoint and code-colour keys are refused: a breakpoint cannot be
// read through var() in a media query, and the code palette is the
// syntax highlighter's, not an app's.
func ParseToken(key, value string) (any, error) {
	cat := TokenCategory(key)
	name, _ := strings.CutPrefix(key, cat+"-")
	var slot any
	switch cat {
	case "color":
		slot = &Color{Name: name}
	case "size":
		slot = &Size{Name: name}
	case "spacing":
		slot = &Spacing{Name: name}
	case "radii":
		slot = &Radius{Name: name}
	case "stroke":
		slot = &Stroke{Name: name}
	case "leading":
		slot = &LineHeight{Name: name}
	case "tracking":
		slot = &LetterSpacing{Name: name}
	case "opacity":
		slot = &Opacity{Name: name}
	case "font":
		slot = &Font{Name: name}
	case "font-weight":
		slot = &FontWeight{Name: name}
	case "shadow":
		slot = &Shadow{Name: name}
	case "z":
		slot = &ZIndexValue{Name: name}
	case "duration":
		slot = &Duration{Name: name}
	case "easing":
		slot = &Easing{Name: name}
	case "text":
		slot = &FontSize{Name: name}
	default:
		return nil, fmt.Errorf("--%s: an app token's name starts with a token type: %s", key, strings.Join(AppTokenPrefixes(), ", "))
	}
	if !tokenNamePattern.MatchString(name) {
		return nil, fmt.Errorf("--%s: the name after %q must be lower-case words joined by single dashes, starting with a letter", key, cat+"-")
	}
	setters := map[string]tokenSetter{}
	collectSetters(reflect.ValueOf(slot), setters, map[string]bool{}, map[string]bool{})
	if err := setters[key](value); err != nil {
		return nil, fmt.Errorf("--%s: %w", key, err)
	}
	return reflect.ValueOf(slot).Elem().Interface(), nil
}

// AppTokenPrefixes lists the type prefixes an app token may carry, the
// keys ParseToken accepts, each with its trailing dash.
func AppTokenPrefixes() []string {
	return []string{"color-", "size-", "spacing-", "radii-", "stroke-", "leading-", "tracking-", "opacity-", "font-", "font-weight-", "shadow-", "z-", "duration-", "easing-", "text-"}
}

// copyTokenStruct copies a token struct (or the struct a pointer holds)
// into a new pointer the theme owns. The copy is deep: a nested token
// struct held by pointer (or in a slice, map or interface) is copied
// too, so ApplyTokens writing one theme's copy never reaches a pointer
// another theme, or the caller, still holds.
func copyTokenStruct(in any) reflect.Value {
	v := reflect.ValueOf(in)
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			panic("style: Theme.Extend: nil token set")
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		panic(fmt.Sprintf("style: Theme.Extend: %T is not a struct of tokens", in))
	}
	p := reflect.New(v.Type())
	p.Elem().Set(v)
	deepenCopy(p.Elem(), map[uintptr]reflect.Value{})
	return p
}

// deepenCopy replaces every pointer, slice, map and interface reachable
// through v's settable fields with a copy, so v shares no mutable
// memory with the value it was copied from. seen maps an original
// pointer to its copy, keeping shared pointers shared within the copy
// and a cycle from recursing forever. Unexported fields keep the
// shallow copy: reflect cannot set them, and the token walkers skip
// them too.
func deepenCopy(v reflect.Value, seen map[uintptr]reflect.Value) {
	switch v.Kind() {
	case reflect.Struct:
		for i := range v.NumField() {
			if f := v.Field(i); f.CanSet() {
				deepenCopy(f, seen)
			}
		}
	case reflect.Pointer:
		if v.IsNil() {
			return
		}
		if c, ok := seen[v.Pointer()]; ok {
			v.Set(c)
			return
		}
		c := reflect.New(v.Elem().Type())
		seen[v.Pointer()] = c
		c.Elem().Set(v.Elem())
		deepenCopy(c.Elem(), seen)
		v.Set(c)
	case reflect.Slice:
		if v.IsNil() {
			return
		}
		c := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		reflect.Copy(c, v)
		for i := range c.Len() {
			deepenCopy(c.Index(i), seen)
		}
		v.Set(c)
	case reflect.Map:
		if v.IsNil() {
			return
		}
		c := reflect.MakeMapWithSize(v.Type(), v.Len())
		for it := v.MapRange(); it.Next(); {
			e := reflect.New(v.Type().Elem()).Elem()
			e.Set(it.Value())
			deepenCopy(e, seen)
			c.SetMapIndex(it.Key(), e)
		}
		v.Set(c)
	case reflect.Interface:
		if v.IsNil() {
			return
		}
		e := reflect.New(v.Elem().Type()).Elem()
		e.Set(v.Elem())
		deepenCopy(e, seen)
		v.Set(e)
	}
}

// cloneExtensions copies every extension so a theme copy can be written
// (ApplyTokens) without reaching the theme it came from.
func cloneExtensions(exts []any) []any {
	if len(exts) == 0 {
		return nil
	}
	out := make([]any, 0, len(exts))
	for _, e := range exts {
		out = append(out, copyTokenStruct(e).Interface())
	}
	return out
}

// extensionPath names an extension in messages by its struct type.
func extensionPath(v reflect.Value) string {
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return "<nil>"
		}
		v = v.Elem()
	}
	return v.Type().String()
}

// duplicateTokenKey reports the first key two tokens of t both emit.
func duplicateTokenKey(t Theme) error {
	var all []tokenAt
	walkTokenPaths(reflect.ValueOf(t), "Theme", &all)
	seen := map[string]string{}
	for _, a := range all {
		if prev, ok := seen[a.Key]; ok {
			return fmt.Errorf("%s and %s both emit --%s; rename one", prev, a.Path, a.Key)
		}
		seen[a.Key] = a.Path
	}
	return nil
}

// tokenAt is one emitted key and the Go field path that emits it, for
// the messages that name both owners of a key.
type tokenAt struct {
	Key, Path string
}

// walkTokenPaths is walkTokens with the field path kept: same skip
// rules (tokenPair decides), same recursion, an extension named by its
// struct type.
func walkTokenPaths(v reflect.Value, path string, out *[]tokenAt) {
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return
		}
		v = v.Elem()
	}
	if v.Kind() == reflect.Slice {
		for i := range v.Len() {
			walkTokenPaths(v.Index(i), extensionPath(v.Index(i)), out)
		}
		return
	}
	if v.Kind() != reflect.Struct {
		return
	}
	if key, _, ok := tokenPair(v); ok {
		*out = append(*out, tokenAt{key, path})
		return
	}
	for sf, f := range v.Fields() {
		if !f.CanInterface() {
			continue
		}
		walkTokenPaths(f, path+"."+sf.Name, out)
	}
}
