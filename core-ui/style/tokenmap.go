package style

import (
	"fmt"
	"maps"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ThemeToTokens flattens t into a map keyed by the CSS custom-property
// identifier WITHOUT the leading "--": "color-primary", "spacing-md",
// "duration-fast", "tk-kw". The value is exactly what CSSCustomProperties
// emits after the colon, "#4F46E5", "8px", "150ms", "var(--color-code-text)".
//
// That key is chosen deliberately over one derived from the Go field path:
// it is (1) exactly what the :root CSS emits, (2) exactly what a theme-
// configurator UI control edits, and (3) stable across Go struct
// reorganisations, a field renamed or reordered changes no key, so a theme
// file or an embed payload keeps addressing the same token.
//
// The typed-token portion goes through the SAME reflection walk as
// CSSCustomPropertiesOf (walkTokens), so the token map and the CSS emitter
// can never disagree about which tokens exist or what value each carries.
//
// # DarkColors / DarkCode key scheme
//
// DarkColors / DarkCode are map[string]string, deliberately skipped by the
// typed token walk (see Theme.DarkColors). They are flattened under the
// "dark." prefix because the light and dark values for one token SHARE the
// CSS custom-property name, --color-primary is declared in :root and
// re-declared in the dark-scheme block, so without a prefix the two would
// collide in the map. The scheme:
//
//	DarkColors["primary"]      → "dark.color-primary"
//	DarkColors["surface-soft"] → "dark.color-surface-soft"
//	DarkCode["kw"]             → "dark.tk-kw"
//
// The "dark." prefix is unambiguous because no typed token key ever contains
// a ".". It round-trips: ApplyTokens strips "dark." then maps the remainder
// ("color-<name>" / "tk-<name>") back onto the right dark map.
func ThemeToTokens(t Theme) map[string]string {
	out := make(map[string]string, 96)
	var pairs []tokenKV
	walkTokens(reflect.ValueOf(t), &pairs)
	for _, p := range pairs {
		out[p.Key] = p.Value
	}
	// Dark entries are inserted in sorted name order. A map is unordered,
	// but a stable insertion order keeps debug dumps readable and makes any
	// future ordered-emit trivial, the round-trip property does not depend
	// on it (ThemeHash is computed from the byte-stable CSS, not the map).
	for _, name := range sortedStringKeys(t.DarkColors) {
		out["dark.color-"+name] = t.DarkColors[name]
	}
	for _, name := range sortedStringKeys(t.DarkCode) {
		out["dark.tk-"+name] = t.DarkCode[name]
	}
	return out
}

// ApplyTokens returns a copy of base with the supplied tokens applied.
//
// Every value reaches CSS, so this function is a security boundary, not a
// convenience: treat the input as untrusted (embedded surfaces do, and a
// theme-configurator file is transcribed text). It fails closed on every
// axis:
//
//   - An UNKNOWN key is an error. A typo in a theme file must be reported,
//     and an embed must not be able to probe for which keys a host accepts.
//   - A value must validate for its token's TYPE: colors against a bounded
//     grammar, integer/duration tokens against their numeric format, and
//     every free-form string (Font, Shadow, Easing, FontSize, CodeColor)
//     against the declaration-breaker rejection that prevents CSS escape.
//
// Colors are the dangerous case: a value like "red; --x:}body{...}" escapes
// its declaration, and CSS alone can exfiltrate via attribute selectors and
// background-image URLs. Colors therefore accept ONLY the bounded grammar
// (hex, rgb()/rgba(), hsl()/hsla(), oklch()/oklab(), color-mix(),
// var(--…), and the CSS named colors) and reject everything else, never
// sanitise by stripping, always reject.
//
// On any error ApplyTokens returns the zero Theme and a non-nil error
// naming the offending key and the reason, in the voice of Theme.Validate
// ("theme: token %q: <reason>"). The supplied base is never mutated: the
// dark maps are deep-copied so one caller's overrides cannot leak into the
// process-global base theme shared across requests.
func ApplyTokens(base Theme, tokens map[string]string) (Theme, error) {
	result := base
	// Deep-copy the dark maps. Without this, writing result.DarkColors
	// would mutate base's map (maps are reference types) and leak one
	// caller's overrides into the base theme, which is process-global and
	// shared across concurrent requests in the theme-variant host.
	result.DarkColors = copyStringMap(base.DarkColors)
	result.DarkCode = copyStringMap(base.DarkCode)

	// One reflection walk over the addressable result builds a validating
	// setter per typed token, keyed by the same CSS-var name ThemeToTokens
	// emits. Walking the result (not base) captures settable field
	// references that write in place.
	setters := make(map[string]tokenSetter, 96)
	lightColorNames := make(map[string]bool)
	lightCodeNames := make(map[string]bool)
	collectSetters(reflect.ValueOf(&result).Elem(), setters, lightColorNames, lightCodeNames)

	// Deterministic ordering: sort keys so map iteration order never decides
	// which error surfaces first.
	keys := make([]string, 0, len(tokens))
	for k := range tokens {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		value := tokens[k]
		if setter, ok := setters[k]; ok {
			if err := setter(value); err != nil {
				return Theme{}, fmt.Errorf("theme: token %q: %w", k, err)
			}
			continue
		}
		// dark.color-<name> / dark.tk-<name>: overrides for the dark-scheme
		// block, which re-declares the same --color-/--tk- vars in a
		// different selector scope.
		if rest, ok := strings.CutPrefix(k, "dark."); ok {
			if err := applyDark(rest, value, lightColorNames, lightCodeNames, &result.DarkColors, &result.DarkCode); err != nil {
				return Theme{}, fmt.Errorf("theme: token %q: %w", k, err)
			}
			continue
		}
		return Theme{}, fmt.Errorf("theme: unknown token %q, not a key this theme exposes (see ThemeToTokens)", k)
	}
	return result, nil
}

// tokenSetter validates one value and writes it to a specific token slot on
// the result theme. collectSetters builds one per typed token.
type tokenSetter func(value string) error

// collectSetters walks the addressable result theme and registers a
// validating setter for every typed token, keyed by the same CSS-var name
// walkTokens/ThemeToTokens use. It is the inverse of walkTokens: same walk,
// same key derivation, but it captures settable field references instead of
// reading values. It also records the sets of light color and code-color
// token names so ApplyTokens can decide whether a "dark.color-<name>"
// override targets a real token.
func collectSetters(v reflect.Value, setters map[string]tokenSetter, lightColors, lightCode map[string]bool) {
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return
	}
	switch v.Interface().(type) {
	case Color:
		name, ok := nonEmptyStringField(v, "Name")
		if !ok {
			return
		}
		val := v.FieldByName("Value")
		lightColors[name] = true
		setters["color-"+name] = func(s string) error {
			if err := validateColorValue(s); err != nil {
				return err
			}
			val.SetString(s)
			return nil
		}
		return
	case Spacing:
		registerIntPxSetter(v, "spacing-", setters)
		return
	case Radius:
		registerIntPxSetter(v, "radii-", setters)
		return
	case Breakpoint:
		name, ok := nonEmptyStringField(v, "Name")
		if !ok {
			return
		}
		val := v.FieldByName("Value")
		setters["breakpoint-"+name] = func(s string) error {
			n, err := parseIntPx(s)
			if err != nil {
				return err
			}
			if n <= 0 {
				return fmt.Errorf("breakpoint must be > 0px (got %q)", s)
			}
			val.SetInt(int64(n))
			return nil
		}
		return
	case Font:
		registerStringSetter(v, "font-", "Font", setters)
		return
	case Shadow:
		registerStringSetter(v, "shadow-", "Shadow", setters)
		return
	case ZIndexValue:
		name, ok := nonEmptyStringField(v, "Name")
		if !ok {
			return
		}
		val := v.FieldByName("Value")
		setters["z-"+name] = func(s string) error {
			n, err := parseIntUnitless(s)
			if err != nil {
				return err
			}
			val.SetInt(int64(n))
			return nil
		}
		return
	case Duration:
		name, ok := nonEmptyStringField(v, "Name")
		if !ok {
			return
		}
		val := v.FieldByName("Value")
		setters["duration-"+name] = func(s string) error {
			d, err := time.ParseDuration(s)
			if err != nil {
				return fmt.Errorf("not a CSS duration (expected e.g. %q): %w", "150ms", err)
			}
			if d <= 0 {
				return fmt.Errorf("duration must be > 0 (got %q)", s)
			}
			val.SetInt(int64(d))
			return nil
		}
		return
	case Easing:
		registerStringSetter(v, "easing-", "Easing", setters)
		return
	case FontSize:
		registerStringSetter(v, "text-", "FontSize", setters)
		return
	case CodeColor:
		name, ok := nonEmptyStringField(v, "Name")
		if !ok {
			return
		}
		val := v.FieldByName("Value")
		lightCode[name] = true
		// CodeColor is optional and only EMITS when Value != "", but a
		// configurator may set the value on a slot that already has a Name;
		// register a setter whenever Name is present so "tk-<name>" stays
		// addressable. CodeColor values reach CSS as --tk-<name> inside the
		// (possibly dark) :root block, so they get the free-form-string
		// check (reject declaration-breakers), not the color grammar.
		setters["tk-"+name] = func(s string) error {
			if err := validateFreeFormCSS(s); err != nil {
				return err
			}
			val.SetString(s)
			return nil
		}
		return
	}
	// Recurse into struct fields.
	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		if !f.CanInterface() {
			continue
		}
		// Skip the bookkeeping `Name string` on Theme itself, it is not a
		// token and carries no CSS-var key.
		if v.Type().Field(i).Name == "Name" && f.Kind() == reflect.String {
			continue
		}
		collectSetters(f, setters, lightColors, lightCode)
	}
}

func registerIntPxSetter(v reflect.Value, prefix string, setters map[string]tokenSetter) {
	name, ok := nonEmptyStringField(v, "Name")
	if !ok {
		return
	}
	val := v.FieldByName("Value")
	setters[prefix+name] = func(s string) error {
		n, err := parseIntPx(s)
		if err != nil {
			return err
		}
		val.SetInt(int64(n))
		return nil
	}
}

func registerStringSetter(v reflect.Value, prefix, typeName string, setters map[string]tokenSetter) {
	name, ok := nonEmptyStringField(v, "Name")
	if !ok {
		return
	}
	val := v.FieldByName("Value")
	setters[prefix+name] = func(s string) error {
		if err := validateFreeFormCSS(s); err != nil {
			return fmt.Errorf("%s: %w", typeName, err)
		}
		val.SetString(s)
		return nil
	}
}

// nonEmptyStringField returns the string held by a struct field by name, or
// ("", false) if the field is missing or empty.
func nonEmptyStringField(v reflect.Value, name string) (string, bool) {
	f := v.FieldByName(name)
	if !f.IsValid() {
		return "", false
	}
	s := f.String()
	if s == "" {
		return "", false
	}
	return s, true
}

// applyDark validates and writes a dark-scheme override. key is the CSS-var
// name AFTER the "dark." prefix ("color-primary" / "tk-kw"). A dark override
// may target any token that has a light counterpart (so the re-declaration
// overrides something real) OR an entry already present in the dark map
// (so a host can mutate an existing override whose light token is absent,
// e.g. a code-only DarkCode on a theme without a typed Code group). The
// value must pass the same Color grammar as a light Color, it reaches CSS
// as "--color-<name>: <value>" inside the dark-scheme block.
func applyDark(key, value string, lightColors, lightCode map[string]bool, darkColors, darkCode *map[string]string) error {
	if name, ok := strings.CutPrefix(key, "color-"); ok {
		if !lightColors[name] && !mapHasKey(*darkColors, name) {
			return fmt.Errorf("dark override targets %q which is not a color token in this theme", name)
		}
		if err := validateColorValue(value); err != nil {
			return err
		}
		ensureMap(darkColors)[name] = value
		return nil
	}
	if name, ok := strings.CutPrefix(key, "tk-"); ok {
		if !lightCode[name] && !mapHasKey(*darkCode, name) {
			return fmt.Errorf("dark override targets %q which is not a code-color token in this theme", name)
		}
		if err := validateColorValue(value); err != nil {
			return err
		}
		ensureMap(darkCode)[name] = value
		return nil
	}
	return fmt.Errorf("dark.* keys must be dark.color-<name> or dark.tk-<name>")
}

func ensureMap(m *map[string]string) map[string]string {
	if *m == nil {
		*m = make(map[string]string)
	}
	return *m
}

func mapHasKey(m map[string]string, k string) bool {
	if m == nil {
		return false
	}
	_, ok := m[k]
	return ok
}

func copyStringMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	maps.Copy(out, m)
	return out
}

func sortedStringKeys(m map[string]string) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// --- value validation -----------------------------------------------------

// cssDeclBreakers lists the byte sequences that, present in any token
// value, let the value escape its CSS custom-property declaration. CSS
// custom properties accept nearly arbitrary text: ";" ends the declaration,
// "}" closes the :root block, and a single one of either is behind every
// theme-value injection. Rejecting this set is the universal security
// boundary for every value that reaches CSS, color or otherwise.
//
// "url(" is rejected even inside otherwise-valid values because CSS loads
// remote resources from url() (a data-exfiltration channel that needs no
// declaration break). "<", ">", and the comment markers are rejected so a
// value can never close a surrounding <style> element or inject a comment
// that toggles parsing. "\" is the CSS escape character and is rejected to
// deny obfuscation. Newlines end a CSS declaration in some parsing contexts.
//
// This is the same property blueprint_emitter_injection_test.go encodes for
// Go literals ("no input may terminate the literal it is emitted into"),
// transposed to CSS.
var cssDeclBreakers = []string{
	";", "}", "{", "/*", "*/", "<", ">", "\\", "\n", "\r", "url(",
}

// validateFreeFormCSS is the type check for free-form string tokens (Font,
// Shadow, Easing, FontSize, CodeColor): reject any value carrying a
// declaration-breaking sequence. Quotes and commas are allowed, font stacks
// and shadow/cubic-bezier expressions need them. An empty value is rejected
// too: clearing a token silently is how a theme ends up with a missing
// --color-* and broken inheritance.
func validateFreeFormCSS(v string) error {
	if v == "" {
		return fmt.Errorf("value is empty")
	}
	if p := findDeclBreaker(v); p != "" {
		return fmt.Errorf("value contains forbidden sequence %q (declaration-breaking)", p)
	}
	return nil
}

// validateColorValue enforces the bounded Color grammar. It first applies
// the universal declaration-breaker rejection (every value reaching CSS must
// pass it), then requires the value to be one of: a hex literal
// (#RGB / #RGBA / #RRGGBB / #RRGGBBAA), a call to one of the accepted color
// functions, var(--…), or a CSS named color. Anything else is rejected,
// Color values are the classic injection vector and a loose grammar is the
// hole.
func validateColorValue(v string) error {
	if v == "" {
		return fmt.Errorf("color value is empty")
	}
	if p := findDeclBreaker(v); p != "" {
		return fmt.Errorf("color value contains forbidden sequence %q (declaration-breaking)", p)
	}
	if !isValidColor(v) {
		return fmt.Errorf("not a recognized color. Accept hex (#RRGGBB), rgb()/rgba(), hsl()/hsla(), oklch()/oklab(), color-mix(), var(--…), or a named color")
	}
	return nil
}

// ValidateColorValue is the exported form of the color-token grammar, for
// callers that write a [Theme]'s color fields DIRECTLY instead of going through
// [ApplyTokens]. Every setter path already runs it; a direct struct assignment
// (`theme.Colors.Primary.Value = v`, `theme.DarkColors[k] = v`) runs nothing at
// all, so any producer that assigns must call this itself.
//
// The blueprint generator in cmd/gofastr is exactly that producer: it emits
// those assignments as generated Go from `app.theme` / `app.theme.dark` in
// gofastr.yml, and the value lands verbatim in `--color-<token>` on the
// app-wide stylesheet the UI host and the admin battery serve.
//
// Exported rather than reimplemented on purpose: "what breaks a CSS
// declaration" (cssDeclBreakers) and "what is a color" (isValidColor) must
// have exactly ONE definition, and it belongs here, beside the token types
// that own the CSS output. A second copy in the generator is a second thing to
// forget to harden, that duplication was already made and unwound once on
// this branch.
//
// Free-form string tokens (Font, Shadow, Easing) have a looser grammar; they
// are validated by validateFreeFormCSS and are not exported here because no
// out-of-package producer assigns them directly yet.
func ValidateColorValue(v string) error { return validateColorValue(v) }

// findDeclBreaker returns the first declaration-breaking sequence found in v,
// or "" if none. Used both to reject and to name the offender in the error.
//
// The comparison is CASE-INSENSITIVE. CSS function names are case-insensitive,
// so a case-sensitive check would reject "url(" while accepting "Url(" and
// "URL(", a boundary that reads as airtight and is trivially stepped around.
// The punctuation breakers are unaffected by folding; only the function-name
// entries need it, and folding everything keeps the rule single-branched.
func findDeclBreaker(v string) string {
	lower := strings.ToLower(v)
	for _, b := range cssDeclBreakers {
		if strings.Contains(lower, b) {
			return b
		}
	}
	return ""
}

// colorFuncPrefixes are the CSS function notations accepted in a Color
// value.
var colorFuncPrefixes = []string{
	"rgb(", "rgba(", "hsl(", "hsla(", "oklab(", "oklch(", "color-mix(", "var(",
}

// allowedCSSFuncs is the set of function names permitted ANYWHERE inside a
// colour value, including nested inside var() fallbacks and color-mix()
// arguments. Anything else is rejected.
//
// This is an allow-list, not a deny-list, and that is the whole point. A
// deny-list of resource-loading functions can always be stepped around: "url("
// is the obvious one, but image-set(), -webkit-image-set(), cross-fade(),
// element(), paint() and src() all fetch or reference external content, and
// `var(--missing, image-set("https://attacker/x" 1x))` reaches a
// background-image sink without the string "url(" appearing anywhere. Naming
// what is allowed makes the set closed instead of a guessing game.
var allowedCSSFuncs = map[string]bool{
	"rgb": true, "rgba": true, "hsl": true, "hsla": true,
	"oklab": true, "oklch": true, "lab": true, "lch": true,
	"hwb": true, "color": true, "color-mix": true, "var": true,
	// Arithmetic is legitimate inside colour components.
	"calc": true, "clamp": true, "min": true, "max": true,
}

// cssFuncCall matches an identifier immediately followed by "(", a function
// invocation. Leading "-" is included so vendor-prefixed forms
// (-webkit-image-set) are seen and rejected rather than skipped.
var cssFuncCall = regexp.MustCompile(`(?i)(-?[a-z][a-z0-9-]*)\s*\(`)

// onlyAllowedFuncs reports whether every function invoked anywhere in v is in
// allowedCSSFuncs.
func onlyAllowedFuncs(v string) bool {
	for _, m := range cssFuncCall.FindAllStringSubmatch(v, -1) {
		if !allowedCSSFuncs[strings.ToLower(m[1])] {
			return false
		}
	}
	return true
}

// closesAtEnd reports whether the parenthesis opened at index open has its
// matching close as the FINAL character of v, i.e. v is ONE function call and
// not a call followed by more content.
//
// Without this, "rgb(0 0 0) URL(https://attacker/x)" passes a
// prefix+suffix+balanced check: it starts with an allowed prefix, ends with
// ")", and its parens balance. Emitted as --color-primary, a
// `background: var(--color-primary)` then resolves to a colour AND an image,
// fetching the attacker's URL.
func closesAtEnd(v string, open int) bool {
	depth := 0
	for i := open; i < len(v); i++ {
		switch v[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i == len(v)-1
			}
		}
	}
	return false
}

func isValidColor(v string) bool {
	// Hex.
	if v[0] == '#' {
		d := v[1:]
		switch len(d) {
		case 3, 4, 6, 8:
			return isHexDigits(d)
		}
		return false
	}
	// Function call. Three independent conditions, all required:
	//   1. a known colour-producing prefix
	//   2. that call's parenthesis closes at the very END of the value, so the
	//      value is exactly one invocation with no trailing content
	//   3. every nested function is on the allow-list, so a fallback or
	//      argument cannot smuggle in a resource-loading function
	if strings.HasSuffix(v, ")") && balancedParens(v) && onlyAllowedFuncs(v) {
		for _, p := range colorFuncPrefixes {
			if !strings.HasPrefix(strings.ToLower(v), p) {
				continue
			}
			if !closesAtEnd(v, len(p)-1) {
				return false
			}
			return strings.TrimSpace(v[len(p):len(v)-1]) != ""
		}
	}
	// Named color.
	return cssNamedColors[strings.ToLower(v)]
}

func isHexDigits(s string) bool {
	for i := range s {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// balancedParens reports whether every "(" in s is matched by a later ")"
// and no ")" precedes its "(". The value has already passed the
// declaration-breaker rejection, so this is a structural sanity check on
// function-call color values, e.g. color-mix(in srgb, var(--x) 15%, white).
func balancedParens(s string) bool {
	depth := 0
	for i := range s {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth < 0 {
				return false
			}
		}
	}
	return depth == 0
}

// parseIntPx parses an integer pixel value like "8px" / "-4px". The "px"
// suffix is required, it is what the :root emitter writes and what
// ThemeToTokens therefore carries, so accepting a bare integer here would
// let a value silently change meaning when the key prefix is wrong (a "8"
// intended for spacing landing on a z-index slot).
func parseIntPx(s string) (int, error) {
	if !strings.HasSuffix(s, "px") {
		return 0, fmt.Errorf("expected an integer pixel value like %q (got %q)", "8px", s)
	}
	n, err := strconv.Atoi(s[:len(s)-2])
	if err != nil {
		return 0, fmt.Errorf("not an integer pixel value: %q", s)
	}
	return n, nil
}

// parseIntUnitless parses a bare integer like "100" (z-index). It rejects a
// "px" suffix so a value intended for a spacing slot can't be misapplied to
// a z-index slot.
func parseIntUnitless(s string) (int, error) {
	if strings.HasSuffix(s, "px") {
		return 0, fmt.Errorf("z-index is a unitless integer (got %q; did you mean a spacing token?)", s)
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("not an integer: %q", s)
	}
	return n, nil
}

// cssNamedColors is the fixed allowlist of CSS named colors (Color Module
// Level 4) plus "transparent" and "currentcolor". A Color value matching one
// of these case-insensitively is accepted; named colors outside this set are
// rejected so a value can't smuggle an arbitrary identifier through the
// color grammar. Stored lower-cased; isValidColor lower-cases its input.
var cssNamedColors = func() map[string]bool {
	names := []string{
		"transparent", "currentcolor",
		"aliceblue", "antiquewhite", "aqua", "aquamarine", "azure",
		"beige", "bisque", "black", "blanchedalmond", "blue", "blueviolet", "brown", "burlywood",
		"cadetblue", "chartreuse", "chocolate", "coral", "cornflowerblue", "cornsilk", "crimson", "cyan",
		"darkblue", "darkcyan", "darkgoldenrod", "darkgray", "darkgreen", "darkgrey", "darkkhaki",
		"darkmagenta", "darkolivegreen", "darkorange", "darkorchid", "darkred", "darksalmon",
		"darkseagreen", "darkslateblue", "darkslategray", "darkslategrey", "darkturquoise", "darkviolet",
		"deeppink", "deepskyblue", "dimgray", "dimgrey", "dodgerblue",
		"firebrick", "floralwhite", "forestgreen", "fuchsia",
		"gainsboro", "ghostwhite", "gold", "goldenrod", "gray", "green", "greenyellow", "grey",
		"honeydew", "hotpink",
		"indianred", "indigo", "ivory",
		"khaki",
		"lavender", "lavenderblush", "lawngreen", "lemonchiffon",
		"lightblue", "lightcoral", "lightcyan", "lightgoldenrodyellow",
		"lightgray", "lightgreen", "lightgrey", "lightpink", "lightsalmon", "lightseagreen",
		"lightskyblue", "lightslategray", "lightslategrey", "lightsteelblue", "lightyellow",
		"lime", "limegreen", "linen",
		"magenta", "maroon",
		"mediumaquamarine", "mediumblue", "mediumorchid", "mediumpurple", "mediumseagreen",
		"mediumslateblue", "mediumspringgreen", "mediumturquoise", "mediumvioletred", "midnightblue",
		"mintcream", "mistyrose", "moccasin",
		"navajowhite", "navy",
		"oldlace", "olive", "olivedrab", "orange", "orangered", "orchid",
		"palegoldenrod", "palegreen", "paleturquoise", "palevioletred", "papayawhip", "peachpuff",
		"peru", "pink", "plum", "powderblue", "purple",
		"rebeccapurple", "red", "rosybrown", "royalblue",
		"saddlebrown", "salmon", "sandybrown", "seagreen", "seashell", "sienna", "silver",
		"skyblue", "slateblue", "slategray", "slategrey", "snow", "springgreen", "steelblue",
		"tan", "teal", "thistle", "tomato", "turquoise",
		"violet",
		"wheat", "white", "whitesmoke",
		"yellow", "yellowgreen",
	}
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}()
