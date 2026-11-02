// Package config provides a first-class configuration loader that binds
// environment variables, config files, and secret sources into typed Go
// structs with validation.
//
// Apps currently roll their own os.Getenv calls. This package removes that
// class of bugs with a single typed binding step.
//
// Usage:
//
//	type AppConfig struct {
//	    Port    int    `config:"PORT" default:"8080" validate:"min=1,max=65535"`
//	    DBURL   string `config:"DATABASE_URL" required:"true"`
//	    Debug   bool   `config:"DEBUG" default:"false"`
//	    LogLevel string `config:"LOG_LEVEL" default:"info" validate:"oneof=debug info warn error"`
//	}
//
//	var cfg AppConfig
//	if err := config.Load(&cfg); err != nil {
//	    log.Fatal(err)
//	}
package config

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"os"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// Source provides configuration values. The default source reads from
// environment variables. Custom sources (files, secret managers, etc.)
// implement this interface.
type Source interface {
	// Get returns the value for the given key, or ("", false) if not found.
	Get(key string) (string, bool)
}

// EnvSource reads from environment variables. This is the default source.
type EnvSource struct{}

// Get returns the environment variable for the given key. Distinguishes
// between unset (returns ok=false) and set-but-empty (returns "", true).
// Conflating those breaks defaulting. An explicit empty value used to
// silently fall back to the `default:` tag.
func (EnvSource) Get(key string) (string, bool) {
	return os.LookupEnv(key)
}

// MapSource reads from a static map. Useful for testing.
type MapSource map[string]string

// Get returns the value from the map.
func (m MapSource) Get(key string) (string, bool) {
	v, ok := m[key]
	return v, ok
}

// ChainedSource tries multiple sources in order, returning the first hit.
type ChainedSource []Source

// Get tries each source in order.
func (cs ChainedSource) Get(key string) (string, bool) {
	for _, s := range cs {
		if v, ok := s.Get(key); ok {
			return v, true
		}
	}
	return "", false
}

// ConfigValidator is implemented by config structs that want a
// post-binding validation hook. Validate runs after every field has
// been populated; returning a non-nil error aborts Load.
//
// Renamed from Validator to avoid collision with entity.Entity's own
// Validate() error method. A config struct that doubled as an
// entity would otherwise accidentally satisfy this interface.
type ConfigValidator interface {
	Validate() error
}

// Load populates the config struct from the given sources (defaulting to
// EnvSource if none are provided). Struct fields use `config:"KEY"` tags
// to specify the source key, `default:"VALUE"` for defaults, and
// `required:"true"` for mandatory fields.
//
// Nested struct fields recurse with a SCREAMING_SNAKE prefix derived from
// the parent field name (e.g. `DB DBConfig` reads keys like DB_HOST). A
// nested struct behind a pointer (`DB *DBConfig`) recurses the same way:
// it is allocated and bound when any of its nested keys is present in a
// source, and left nil when none is. A `required` leaf inside a struct
// that stayed nil is therefore not enforced — use the value spelling
// when absence must fail the boot.
//
// The fields of an EMBEDDED struct are promoted the way encoding/json
// promotes them, with no prefix: in `type Cfg struct{ Base }`, Base's
// `config:"PORT"` field reads PORT (never BASE_PORT). That holds for a
// pointer embed (allocated when one of its keys is present, left nil
// otherwise) and for an unexported embedded type's exported fields. A
// shallower field wins over a promoted one with the same key; two
// promoted at the same depth are ambiguous and Load refuses the type.
// Name the field (`Base Base`) to read its keys under BASE_. A config
// tag on an embedded struct is refused.
//
// `required:"true"` demands a non-empty value: a key that is present
// but set to the empty string ("SECRET=" in a .env, a ConfigMap key
// with no value) fails the same way an absent key does. A `default:`
// tag still satisfies a required field when the key is absent.
//
// Fields tagged `sensitive:"true"` have their raw value redacted from
// any error messages, including the text of a ConfigValidator error.
//
// If the config (or a pointer to it) implements ConfigValidator,
// Validate is called after binding and its error is returned.
//
// Supported field types: string, int, int64, float64, bool, Duration,
// and nested structs (value or pointer form).
func Load(cfg any, sources ...Source) error {
	return LoadWith(cfg, sources...)
}

// LoadWith is an alias for Load. Populates config from sources.
func LoadWith(cfg any, sources ...Source) error {
	v := reflect.ValueOf(cfg)
	if v.Kind() != reflect.Pointer || v.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("config: expected pointer to struct, got %T", cfg)
	}

	if err := checkConfigShape(v.Elem().Type(), make(map[reflect.Type]bool)); err != nil {
		return err
	}
	src := ChainedSource(sources)
	if len(sources) == 0 {
		src = ChainedSource{EnvSource{}}
	}

	var secrets []string
	if err := bindStruct(v.Elem(), "", src, &secrets); err != nil {
		return err
	}

	if val, ok := cfg.(ConfigValidator); ok {
		if err := val.Validate(); err != nil {
			// The `sensitive:"true"` contract is "redacted from any error
			// messages", but redaction used to wrap only setField's parse
			// error. A host's Validate() writing the idiomatic
			// fmt.Errorf("bad DSN %q", c.DBURL) was wrapped verbatim and
			// leaked the credential out through MustLoad's panic. We can't
			// restructure a third-party error, so scrub the bound secret
			// values out of its rendered text instead.
			return fmt.Errorf("config: validate: %s", redactSecrets(err.Error(), secrets))
		}
	}
	return nil
}

// redactSecrets replaces every occurrence of a bound sensitive value in
// msg with a placeholder. Values are scrubbed longest-first so a secret
// that contains a shorter one doesn't leave a fragment behind.
//
// Each secret is matched in its escaped spellings too (see
// [secretSpellings]): a validator writing fmt.Errorf("%q", tok) turns a
// '"' or '\' in the token into an escape, and a raw-bytes match then
// missed the whole credential.
func redactSecrets(msg string, secrets []string) string {
	if len(secrets) == 0 {
		return msg
	}
	var ordered []string
	for _, s := range secrets {
		if s != "" {
			ordered = append(ordered, secretSpellings(s)...)
		}
	}
	sort.SliceStable(ordered, func(i, j int) bool { return len(ordered[i]) > len(ordered[j]) })
	for _, s := range ordered {
		msg = strings.ReplaceAll(msg, s, "(redacted)")
	}
	return msg
}

// secretEscapers are the ways an error message commonly re-spells a
// value: Go quoting (%q, %+q, %v of a []string), JSON with and without
// HTML escaping, and URL query/path escaping. Each returns the body
// without surrounding quotes, so a spelling embedded in a longer quoted
// string still matches.
var secretEscapers = []func(string) string{
	func(s string) string { q := strconv.Quote(s); return q[1 : len(q)-1] },
	func(s string) string { q := strconv.QuoteToASCII(s); return q[1 : len(q)-1] },
	func(s string) string { return jsonBody(s, true) },
	func(s string) string { return jsonBody(s, false) },
	url.QueryEscape,
	url.PathEscape,
}

// secretSpellings returns s and its escaped forms, up to two escapes
// deep: a validator error quoted again by a wrapping %q (or JSON
// encoded by a logger) carries the secret doubly escaped.
func secretSpellings(s string) []string {
	seen := map[string]bool{s: true}
	out := []string{s}
	level := []string{s}
	for range 2 {
		var next []string
		for _, v := range level {
			for _, esc := range secretEscapers {
				if e := esc(v); e != "" && !seen[e] {
					seen[e] = true
					out = append(out, e)
					next = append(next, e)
				}
			}
		}
		level = next
	}
	return out
}

// jsonBody is s JSON-encoded as a string, without the quotes.
func jsonBody(s string, escapeHTML bool) string {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(escapeHTML)
	if err := enc.Encode(s); err != nil {
		return ""
	}
	q := strings.TrimSuffix(b.String(), "\n")
	return q[1 : len(q)-1]
}

// configField is one field Load binds in a struct: a leaf it reads one
// key for, or a named nested struct whose fields it reads under key as
// a prefix ("DB_"). Fields of embedded structs are promoted into their
// parent's list, as encoding/json promotes them, so index is the path
// from the struct through each embedded field to this one.
type configField struct {
	index  []int
	key    string
	nested bool
	path   string // Go spelling for errors: "Base.Port"
	sf     reflect.StructField
}

// keyLabel is how an error names the field's key.
func (f configField) keyLabel() string {
	if f.nested {
		return f.key + "*"
	}
	return f.key
}

// isNestedStruct reports whether a named field of type t recurses under
// a NAME_ prefix: a struct, or a pointer to one.
func isNestedStruct(t reflect.Type) bool {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t.Kind() == reflect.Struct
}

// configFields lists the fields Load binds in t, in field order, with
// the fields of embedded structs promoted the way encoding/json promotes
// them: `type Cfg struct{ Base }` reads Base's `config:"PORT"` as PORT.
// An unexported embedded type promotes its exported fields too, and a
// pointer embed is followed.
//
// Two fields reading one key resolve as encoding/json resolves two
// fields with one name: the shallower one wins and the deeper one is not
// read at all. Two at the same depth are ambiguous, and where
// encoding/json drops both silently, Load refuses the type.
//
// A config tag on an embedded struct is refused rather than read as a
// prefix: naming the field (`Base Base`) is how a struct's keys get one.
func configFields(t reflect.Type) ([]configField, error) {
	var all []configField
	if err := collectConfigFields(t, nil, "", map[reflect.Type]bool{t: true}, &all); err != nil {
		return nil, err
	}
	byKey := map[string][]configField{}
	var keys []string
	for _, f := range all {
		if _, seen := byKey[f.key]; !seen {
			keys = append(keys, f.key)
		}
		byKey[f.key] = append(byKey[f.key], f)
	}
	out := make([]configField, 0, len(keys))
	for _, k := range keys {
		group := byKey[k]
		depth := len(group[0].index)
		for _, f := range group[1:] {
			depth = min(depth, len(f.index))
		}
		var winners []configField
		for _, f := range group {
			if len(f.index) == depth {
				winners = append(winners, f)
			}
		}
		if len(winners) > 1 {
			paths := make([]string, len(winners))
			for i, f := range winners {
				paths[i] = f.path
			}
			return nil, fmt.Errorf("config: %s: key %s is ambiguous: %s are promoted from embedded structs at the same depth; give one a different config tag, or declare the field on %s itself so it wins",
				t, winners[0].keyLabel(), strings.Join(paths, " and "), t)
		}
		out = append(out, winners[0])
	}
	slices.SortFunc(out, func(a, b configField) int { return slices.Compare(a.index, b.index) })
	return out, nil
}

// collectConfigFields appends every candidate field of t, descending into
// embedded structs. active holds the embedded types on the current path,
// so a type that embeds itself is refused instead of walked forever.
func collectConfigFields(t reflect.Type, index []int, path string, active map[reflect.Type]bool, out *[]configField) error {
	for i := range t.NumField() {
		f := t.Field(i)
		idx := append(slices.Clone(index), i)
		p := f.Name
		if path != "" {
			p = path + "." + f.Name
		}
		if f.Anonymous {
			ft := f.Type
			if ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				if tag := f.Tag.Get("config"); tag != "" {
					return fmt.Errorf("config: %s: embedded struct %s has config tag %q, but an embedded struct's fields are promoted and read without a prefix; name the field (%s %s) to read them under %s_",
						t, p, tag, f.Name, f.Type, strings.ToUpper(f.Name))
				}
				if active[ft] {
					return fmt.Errorf("config: recursive struct %s is unsupported", ft)
				}
				active[ft] = true
				err := collectConfigFields(ft, idx, p, active, out)
				delete(active, ft)
				if err != nil {
					return err
				}
				continue
			}
			// An embedded non-struct is a field named after its type, as
			// in encoding/json; an unexported one is not read.
		}
		if !f.IsExported() {
			continue
		}
		if isNestedStruct(f.Type) {
			*out = append(*out, configField{index: idx, key: strings.ToUpper(f.Name) + "_", nested: true, path: p, sf: f})
			continue
		}
		key := f.Tag.Get("config")
		if key == "" {
			key = strings.ToUpper(f.Name)
		}
		*out = append(*out, configField{index: idx, key: key, path: p, sf: f})
	}
	return nil
}

// srcHasKey reports whether src holds any key the fields of t would read
// at prefix — leaves at prefix+key (tag or uppercased name), nested
// structs at prefix+NAME_…, promoted fields without their embed's name —
// mirroring bindStruct's key derivation exactly (both read configFields).
// It is the presence probe that decides whether a nil pointer-to-struct
// field, or a nil pointer embed, is allocated or left nil.
func srcHasKey(t reflect.Type, prefix string, src Source) bool {
	fields, err := configFields(t)
	if err != nil {
		return false // checkConfigShape refused the type before any probe
	}
	for _, f := range fields {
		if fieldHasKey(f, prefix, src) {
			return true
		}
	}
	return false
}

// fieldHasKey reports whether src holds a key f reads at prefix.
func fieldHasKey(f configField, prefix string, src Source) bool {
	if f.nested {
		t := f.sf.Type
		if t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		return srcHasKey(t, prefix+f.key, src)
	}
	_, ok := src.Get(prefix + f.key)
	return ok
}

// allocEmbeds allocates every nil pointer embed on f's path in elem. An
// unexported one cannot be allocated through reflect (encoding/json
// refuses it the same way), so a key present behind one is an error
// rather than a silently dropped value.
func allocEmbeds(elem reflect.Value, f configField, prefix string) error {
	v := elem
	for _, i := range f.index[:len(f.index)-1] {
		v = v.Field(i)
		if v.Kind() != reflect.Pointer {
			continue
		}
		if v.IsNil() {
			if !v.CanSet() {
				return fmt.Errorf("config: field %s: key %s is set, but the embedded pointer to unexported struct %s is nil and cannot be allocated; embed it by value, or set the pointer before Load",
					f.path, prefix+f.keyLabel(), v.Type().Elem())
			}
			v.Set(reflect.New(v.Type().Elem()))
		}
		v = v.Elem()
	}
	return nil
}

// reachField returns the field at f's path in elem, or false when a nil
// pointer embed on the path leaves it unreachable.
func reachField(elem reflect.Value, f configField) (reflect.Value, bool) {
	v := elem
	for n, i := range f.index {
		if n > 0 && v.Kind() == reflect.Pointer {
			if v.IsNil() {
				return reflect.Value{}, false
			}
			v = v.Elem()
		}
		v = v.Field(i)
	}
	return v, true
}

// bindStruct walks the fields of elem, reading from src under the given
// prefix. Named nested struct fields recurse with prefix+FieldName+"_";
// fields of embedded structs are promoted (see [configFields]).
//
// Every value bound into a `sensitive:"true"` field is appended to
// secrets so [redactSecrets] can scrub it out of a ConfigValidator's
// error text.
func bindStruct(elem reflect.Value, prefix string, src Source, secrets *[]string) error {
	fields, err := configFields(elem.Type())
	if err != nil {
		return err
	}
	// A nil pointer embed is allocated when any field promoted through it
	// has a key present, before any field binds, so a required or
	// defaulted field ahead of the present one in the embed is still
	// enforced. One with no key present stays nil and its fields are
	// skipped, as a nil `DB *DBConfig` is.
	for _, f := range fields {
		if fieldHasKey(f, prefix, src) {
			if err := allocEmbeds(elem, f, prefix); err != nil {
				return err
			}
		}
	}

	for _, cf := range fields {
		field := cf.sf
		fieldVal, ok := reachField(elem, cf)
		if !ok || !fieldVal.CanSet() {
			continue
		}

		if cf.nested {
			nestedPrefix := prefix + cf.key
			// A nested struct behind a POINTER recurses like the value
			// form: allocated and bound when any of its nested keys is
			// present, left nil otherwise. It used to fall through to the
			// leaf path, whose uppercase-name key is almost never set, so
			// `DB *DBConfig` silently ignored DB_HOST/DB_PORT while Load
			// returned nil (2026-09-04 red-probe round).
			if fieldVal.Kind() == reflect.Pointer {
				if fieldVal.IsNil() {
					if !srcHasKey(fieldVal.Type().Elem(), nestedPrefix, src) {
						continue // no nested key present: leave the pointer nil
					}
					fieldVal.Set(reflect.New(fieldVal.Type().Elem()))
				}
				fieldVal = fieldVal.Elem()
			}
			if err := bindStruct(fieldVal, nestedPrefix, src, secrets); err != nil {
				return err
			}
			continue
		}

		key := prefix + cf.key

		required := field.Tag.Get("required") == "true"
		defaultVal := field.Tag.Get("default")
		sensitive := field.Tag.Get("sensitive") == "true"

		val, found := src.Get(key)
		if !found {
			val = defaultVal
		}

		// `required` means a usable value, not merely a present key.
		// EnvSource deliberately reports set-but-empty as ("", true), so
		// checking only !found let `SECRET=`, an empty .env line, a k8s
		// ConfigMap key with no value, a secret-manager miss, satisfy
		// `required` and silently bind the zero value. Test the resolved
		// value instead of the lookup's ok flag.
		if val == "" {
			if required {
				return fmt.Errorf("config: required field %s (%s) not set", field.Name, key)
			}
			if err := validateTags(fieldVal, field.Tag.Get("validate")); err != nil {
				return fmt.Errorf("config: field %s: %s", field.Name, redactSecrets(err.Error(), *secrets))
			}
			// An empty value (`KEY=`) leaves the field as the host set
			// it; the tags still check what it holds.
			continue
		}

		if sensitive {
			*secrets = append(*secrets, val)
		}

		if err := setField(fieldVal, val, field.Name); err != nil {
			if sensitive {
				return fmt.Errorf("config: field %s: invalid sensitive value (redacted)", field.Name)
			}
			return err
		}
		if err := validateTags(fieldVal, field.Tag.Get("validate")); err != nil {
			return fmt.Errorf("config: field %s: %s", field.Name, redactSecrets(err.Error(), *secrets))
		}
	}

	return nil
}

// setField sets a reflect.Value from a string, converting to the
// appropriate type.
func setField(v reflect.Value, s string, fieldName string) error {
	switch v.Kind() {
	case reflect.String:
		v.SetString(s)
	case reflect.Int, reflect.Int64:
		// Check for time.Duration
		if v.Type().String() == "time.Duration" {
			d, err := parseDuration(s)
			if err != nil {
				return fmt.Errorf("config: field %s: %q is not a valid duration: %w", fieldName, s, err)
			}
			v.SetInt(int64(d))
			return nil
		}
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return fmt.Errorf("config: field %s: %q is not a valid integer: %w", fieldName, s, err)
		}
		v.SetInt(n)
	case reflect.Float64:
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return fmt.Errorf("config: field %s: %q is not a valid float: %w", fieldName, s, err)
		}
		// NaN/Inf defeat every comparison guard a host can write (IEEE-754:
		// every comparison with NaN is false, Inf compares true on one side),
		// so `if r < 0 || r > 1 { error }` in a Validate() hook passes NaN
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return fmt.Errorf("config: field %s: %q is not a valid float: non-finite values are rejected because they bypass numeric range guards", fieldName, s)
		}
		v.SetFloat(f)
	case reflect.Bool:
		b, err := strconv.ParseBool(s)
		if err != nil {
			return fmt.Errorf("config: field %s: %q is not a valid bool: %w", fieldName, s, err)
		}
		v.SetBool(b)
	default:
		return fmt.Errorf("config: field %s: unsupported type %s", fieldName, v.Kind())
	}
	return nil
}

// parseDuration parses a duration string. Supports standard Go duration
// syntax ("30s", "5m", "500ms") plus plain integer seconds.
func parseDuration(s string) (int64, error) {
	// Try standard Go parsing first
	if strings.ContainsAny(s, "hmsuµn") {
		d, err := parseGoDuration(s)
		if err == nil {
			return d, nil
		}
		return 0, err
	}
	// Plain integer = seconds
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, err
	}
	// The seconds-to-nanoseconds multiply overflows int64 past ~292 years,
	// and it wrapped silently: a TTL of math.MaxInt64 bound as -1s, which
	// every consumer reads as "already expired" or "no timeout", depending
	// on which side of zero it checks. Refuse the value instead of binding
	// a number nobody wrote.
	const maxSeconds = math.MaxInt64 / int64(time.Second)
	if n > maxSeconds || n < math.MinInt64/int64(time.Second) {
		return 0, fmt.Errorf("duration %ss overflows int64 nanoseconds (max %d seconds)", s, maxSeconds)
	}
	return n * int64(time.Second), nil
}

// parseGoDuration delegates to time.ParseDuration so all of Go's
// suffixes ("ns", "us"/"µs", "ms", "s", "m", "h") parse correctly.
// The previous handrolled scanner mis-read "500ms" as "500m + trailing s".
func parseGoDuration(s string) (int64, error) {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("%w", err)
	}
	return int64(d), nil
}

// MustLoad is like Load but panics on error. Use in init() or main().
func MustLoad(cfg any, sources ...Source) {
	if err := Load(cfg, sources...); err != nil {
		panic(err)
	}
}

// validateTags enforces the bounds and choices advertised by Load.
func validateTags(v reflect.Value, tag string) error {
	if tag == "" {
		return nil
	}
	f := schema.Field{}
	var value any
	isDuration := v.Type() == reflect.TypeFor[time.Duration]()
	switch v.Kind() {
	case reflect.String:
		f.Type = schema.String
		value = v.String()
	case reflect.Int, reflect.Int64:
		f.Type = schema.Int
		value = v.Int()
	case reflect.Float64:
		f.Type = schema.Float
		value = v.Float()
	default:
		return fmt.Errorf("validation unsupported for %s", v.Kind())
	}
	for _, rule := range strings.Split(tag, ",") {
		key, arg, ok := strings.Cut(rule, "=")
		if !ok {
			return fmt.Errorf("invalid validation rule %q", rule)
		}
		switch key {
		case "min", "max":
			if v.Kind() == reflect.Int || v.Kind() == reflect.Int64 {
				// Integer and Duration bounds compare as int64: a float64
				// bound rounds past 2^53. A Duration's bounds are durations
				// ("1s", "5m"), compared in nanoseconds like the value.
				var b int64
				var err error
				if isDuration {
					b, err = parseDuration(arg)
				} else {
					b, err = strconv.ParseInt(arg, 10, 64)
				}
				if err != nil {
					return fmt.Errorf("invalid %s bound", key)
				}
				if key == "min" && v.Int() < b {
					return fmt.Errorf("must be at least %s", arg)
				}
				if key == "max" && v.Int() > b {
					return fmt.Errorf("must be at most %s", arg)
				}
				continue
			}
			n, err := strconv.ParseFloat(arg, 64)
			if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
				return fmt.Errorf("invalid %s bound", key)
			}
			if key == "min" {
				f.Min = &n
			} else {
				f.Max = &n
			}
		case "oneof":
			if v.Kind() != reflect.String {
				return fmt.Errorf("oneof requires a string")
			}
			valid := false
			for _, choice := range strings.Fields(arg) {
				if v.String() == choice {
					valid = true
				}
			}
			if !valid {
				return fmt.Errorf("must be one of %s", arg)
			}
		default:
			return fmt.Errorf("unknown validation rule %q", key)
		}
	}
	return schema.Validate(f, value)
}

// checkConfigShape refuses, before any source is read or any pointer
// allocated, a struct shape Load cannot bind: a recursive one (key
// discovery would be unbounded, through a named field or an embed), a
// key two embedded structs promote at the same depth, and a config tag
// on an embedded struct. It walks the same field list bindStruct binds
// ([configFields]), so every nested struct Load could reach is checked.
func checkConfigShape(t reflect.Type, active map[reflect.Type]bool) error {
	if active[t] {
		return fmt.Errorf("config: recursive struct %s is unsupported", t)
	}
	active[t] = true
	defer delete(active, t)
	fields, err := configFields(t)
	if err != nil {
		return err
	}
	for _, f := range fields {
		if !f.nested {
			continue
		}
		ft := f.sf.Type
		if ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}
		if err := checkConfigShape(ft, active); err != nil {
			return err
		}
	}
	return nil
}
