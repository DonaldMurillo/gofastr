package style

import (
	"fmt"
	"maps"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ThemeToTokens flattens t into a map keyed by the CSS custom-property
// identifier WITHOUT the leading "--".
func ThemeToTokens(t Theme) map[string]string {
	out := make(map[string]string, 96)
	var pairs []tokenKV
	walkTokens(reflect.ValueOf(t), &pairs)
	for _, p := range pairs {
		out[p.Key] = p.Value
	}
	for _, name := range slices.Sorted(maps.Keys(t.DarkColors)) {
		out["dark.color-"+name] = t.DarkColors[name]
	}
	for _, name := range slices.Sorted(maps.Keys(t.DarkCode)) {
		out["dark.tk-"+name] = t.DarkCode[name]
	}
	for _, k := range slices.Sorted(maps.Keys(t.Components)) {
		out["component."+k] = t.Components[k]
	}
	for _, k := range slices.Sorted(maps.Keys(t.Knobs)) {
		out["knob."+k] = t.Knobs[k]
	}
	return out
}

// ApplyTokens returns a copy of base with the supplied tokens applied.
func ApplyTokens(base Theme, tokens map[string]string) (Theme, error) {
	result := base
	result.DarkColors = copyStringMap(base.DarkColors)
	result.DarkCode = copyStringMap(base.DarkCode)
	result.Components = copyStringMap(base.Components)
	result.Knobs = copyStringMap(base.Knobs)
	result.Extensions = cloneExtensions(base.Extensions)

	setters := make(map[string]tokenSetter, 96)
	lightColorNames := make(map[string]bool)
	lightCodeNames := make(map[string]bool)
	fillOptionalDefaults(&result)
	collectSetters(reflect.ValueOf(&result).Elem(), setters, lightColorNames, lightCodeNames)

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
		if rest, ok := strings.CutPrefix(k, "dark."); ok {
			if err := applyDark(rest, value, lightColorNames, lightCodeNames, &result.DarkColors, &result.DarkCode); err != nil {
				return Theme{}, fmt.Errorf("theme: token %q: %w", k, err)
			}
			continue
		}
		if rest, ok := strings.CutPrefix(k, "component."); ok {
			if err := validateComponentEntry(rest, value); err != nil {
				return Theme{}, fmt.Errorf("theme: token %q: %w", k, err)
			}
			result.Components = ensureMap(&result.Components)
			result.Components[rest] = value
			continue
		}
		if rest, ok := strings.CutPrefix(k, "knob."); ok {
			if err := validateKnobs(map[string]string{rest: value}); err != nil {
				return Theme{}, fmt.Errorf("theme: token %q: %w", k, err)
			}
			result.Knobs = ensureMap(&result.Knobs)
			result.Knobs[rest] = value
			continue
		}
		return Theme{}, fmt.Errorf("theme: unknown token %q, not a key this theme exposes (see ThemeToTokens)", k)
	}
	return result, nil
}

type tokenSetter func(value string) error

func collectSetters(v reflect.Value, setters map[string]tokenSetter, lightColors, lightCode map[string]bool) {
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return
		}
		v = v.Elem()
	}
	if v.Kind() == reflect.Slice {
		for i := range v.Len() {
			collectSetters(v.Index(i), setters, lightColors, lightCode)
		}
		return
	}
	if v.Kind() != reflect.Struct {
		return
	}
	switch v.Interface().(type) {
	case Color:
		registerValidatedSetter(v, "color-", lightColors, validateColorValue, setters)
		return
	case Spacing:
		registerIntPxSetter(v, "spacing-", setters)
		return
	case Radius:
		registerIntPxSetter(v, "radii-", setters)
		return
	case Stroke:
		registerValidatedSetter(v, "stroke-", map[string]bool{}, validateStrokeValue, setters)
		return
	case LineHeight:
		registerValidatedSetter(v, "leading-", map[string]bool{}, validateLineHeightValue, setters)
		return
	case LetterSpacing:
		registerValidatedSetter(v, "tracking-", map[string]bool{}, validateLetterSpacingValue, setters)
		return
	case Opacity:
		registerValidatedSetter(v, "opacity-", map[string]bool{}, validateOpacityValue, setters)
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
	case Size:
		registerValidatedSetter(v, "size-", map[string]bool{}, validateSizeValue, setters)
		return
	case FontWeight:
		name, ok := nonEmptyStringField(v, "Name")
		if !ok {
			return
		}
		val := v.FieldByName("Value")
		setters["font-weight-"+name] = func(s string) error {
			n, err := strconv.Atoi(s)
			if err != nil {
				return fmt.Errorf("font weight is a unitless integer (got %q)", s)
			}
			if err := validateFontWeight(n); err != nil {
				return err
			}
			val.SetInt(int64(n))
			return nil
		}
		return
	case CodeColor:
		registerValidatedSetter(v, "tk-", lightCode, validateFreeFormCSS, setters)
		return
	}
	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		if !f.CanInterface() {
			continue
		}
		if v.Type().Field(i).Name == "Name" && f.Kind() == reflect.String {
			continue
		}
		collectSetters(f, setters, lightColors, lightCode)
	}
}

func registerValidatedSetter(v reflect.Value, prefix string, names map[string]bool, validate func(string) error, setters map[string]tokenSetter) {
	name, ok := nonEmptyStringField(v, "Name")
	if !ok {
		return
	}
	val := v.FieldByName("Value")
	names[name] = true
	setters[prefix+name] = func(s string) error {
		if err := validate(s); err != nil {
			return err
		}
		val.SetString(s)
		return nil
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
		if err := validateFreeFormCSS(value); err != nil {
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

var cssDeclBreakers = []string{
	";", "}", "{", "/*", "*/", "<", ">", "\\", "\n", "\r", "url(",
}

func validateFreeFormCSS(v string) error {
	if v == "" {
		return fmt.Errorf("value is empty")
	}
	if p := findDeclBreaker(v); p != "" {
		return fmt.Errorf("value contains forbidden sequence %q (declaration-breaking)", p)
	}
	return nil
}

var cssLength = regexp.MustCompile(`^(0|-?(\d+(\.\d+)?|\.\d+)(px|rem|em|ch|ex|lh|rlh|vw|vh|vi|vb|vmin|vmax|svw|svh|lvw|lvh|dvw|dvh|cqw|cqh|cqi|cqb|cqmin|cqmax|%))$`)

var sizeFuncs = map[string]bool{"calc": true, "clamp": true, "min": true, "max": true, "var": true}

func validateSizeValue(v string) error {
	if err := validateFreeFormCSS(v); err != nil {
		return err
	}
	if cssLength.MatchString(v) {
		return nil
	}
	open := strings.IndexByte(v, '(')
	if open > 0 && sizeFuncs[strings.ToLower(v[:open])] && closesAtEnd(v, open) {
		ok := true
		for _, m := range cssFuncCall.FindAllStringSubmatch(v, -1) {
			if !sizeFuncs[strings.ToLower(m[1])] {
				ok = false
			}
		}
		if ok {
			return nil
		}
	}
	return fmt.Errorf("not a CSS length (expected e.g. %q or %q, got %q)", "66rem", "clamp(20px, 5vw, 32px)", v)
}

var reStrokeLength = regexp.MustCompile(`^(?:0|\d*\.?\d+(?:px|rem|em))$`)

func validateStrokeValue(v string) error {
	if !reStrokeLength.MatchString(v) {
		return fmt.Errorf("a stroke is 0 or a non-negative px/rem/em length (got %q)", v)
	}
	return nil
}

var reLineHeight = regexp.MustCompile(`^(?:\d*\.?\d+)(?:px|rem|em)?$`)

func validateLineHeightValue(v string) error {
	if !reLineHeight.MatchString(v) {
		return fmt.Errorf("a line height is a unitless number or a non-negative px/rem/em length (got %q)", v)
	}
	return nil
}

var reLetterSpacing = regexp.MustCompile(`^(?:0|-?\d*\.?\d+(?:px|rem|em))$`)

func validateLetterSpacingValue(v string) error {
	if !reLetterSpacing.MatchString(v) {
		return fmt.Errorf("a letter spacing is 0 or a px/rem/em length (got %q)", v)
	}
	return nil
}

var reOpacity = regexp.MustCompile(`^(?:0(?:\.\d+)?|\.\d+|1(?:\.0+)?)$`)

func validateOpacityValue(v string) error {
	if !reOpacity.MatchString(v) {
		return fmt.Errorf("an opacity is a number from 0 to 1 (got %q)", v)
	}
	return nil
}

func validateFontWeight(n int) error {
	if n < 1 || n > 1000 {
		return fmt.Errorf("font weight must be 1 to 1000 (got %d)", n)
	}
	return nil
}

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
// callers that write a [Theme]'s color fields DIRECTLY instead of going
// through [ApplyTokens].
func ValidateColorValue(v string) error { return validateColorValue(v) }

func findDeclBreaker(v string) string {
	lower := strings.ToLower(v)
	for _, b := range cssDeclBreakers {
		if strings.Contains(lower, b) {
			return b
		}
	}
	return ""
}

var colorFuncPrefixes = []string{
	"rgb(", "rgba(", "hsl(", "hsla(", "oklab(", "oklch(", "color-mix(", "var(",
}

var allowedCSSFuncs = map[string]bool{
	"rgb": true, "rgba": true, "hsl": true, "hsla": true,
	"oklab": true, "oklch": true, "lab": true, "lch": true,
	"hwb": true, "color": true, "color-mix": true, "var": true,
	"calc": true, "clamp": true, "min": true, "max": true,
}

var cssFuncCall = regexp.MustCompile(`(?i)(-?[a-z][a-z0-9-]*)\s*\(`)

func onlyAllowedFuncs(v string) bool {
	for _, m := range cssFuncCall.FindAllStringSubmatch(v, -1) {
		if !allowedCSSFuncs[strings.ToLower(m[1])] {
			return false
		}
	}
	return true
}

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
	if v[0] == '#' {
		d := v[1:]
		switch len(d) {
		case 3, 4, 6, 8:
			return isHexDigits(d)
		}
		return false
	}
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
