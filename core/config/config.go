// Package config binds environment variables and other sources into typed
// Go structs with defaults, required fields, redaction and validation.
package config

import (
	"encoding"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/url"
	"os"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// Source provides configuration values.
type Source interface {
	// Get returns the value for the given key, or ("", false) if not found.
	Get(key string) (string, bool)
}

// EnvSource reads from environment variables. This is the default source.
type EnvSource struct{}

// Get returns the environment variable, ("", true) when it is set but empty.
func (EnvSource) Get(key string) (string, bool) {
	return os.LookupEnv(key)
}

// MapSource reads from a static map.
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

// ConfigValidator is a config struct's post-binding check; its error aborts
// Load.
type ConfigValidator interface {
	Validate() error
}

// Load binds cfg, a pointer to a struct, from sources (EnvSource when none)
// using its config, default, required, sensitive and validate tags. Named
// nested structs read keys under a NAME_ prefix, embedded structs are
// promoted without one, and a nested pointer is allocated only when one of
// its keys is present. Sensitive values are redacted from every error, a
// ConfigValidator's included. Load errors on a struct shape it cannot bind,
// a missing or empty required value, a value that does not parse or fails
// its validate rule, and a ConfigValidator error.
func Load(cfg any, sources ...Source) error {
	return LoadWith(cfg, sources...)
}

// LoadWith is Load.
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
	warnLegacyKeys(v.Elem().Type(), src)

	var secrets []string
	collectSecrets(v.Elem(), v.Elem().Type(), "", src, false, &secrets)
	slices.Sort(secrets)
	secrets = slices.Compact(secrets)
	if err := bindStruct(v.Elem(), "", src); err != nil {
		msg := err.Error()
		if redacted := redactSecrets(msg, secrets); redacted != msg {
			return fmt.Errorf("%s", redacted)
		}
		return err
	}

	if val, ok := cfg.(ConfigValidator); ok {
		if err := val.Validate(); err != nil {
			return fmt.Errorf("config: validate: %s", redactSecrets(err.Error(), secrets))
		}
	}
	return nil
}

func collectSecrets(v reflect.Value, t reflect.Type, prefix string, src Source, shadowed bool, secrets *[]string) {
	fields, err := configFields(t)
	if err != nil {
		return
	}
	for _, f := range fields {
		var fv reflect.Value
		if v.IsValid() {
			fv, _ = reachField(v, f)
		}
		hidden := shadowed || f.shadowed
		if f.nested {
			ft := f.sf.Type
			if ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
				if fv.IsValid() {
					fv = fv.Elem()
				}
			}
			collectSecrets(fv, ft, prefix+f.key, src, hidden, secrets)
			continue
		}
		if f.sf.Tag.Get("sensitive") != "true" {
			continue
		}
		value := f.sf.Tag.Get("default")
		if !hidden {
			if supplied, ok := src.Get(prefix + f.key); ok {
				value = supplied
			}
		}
		if value != "" {
			*secrets = append(*secrets, value)
			continue
		}
		if fv.IsValid() && fv.CanInterface() && !fv.IsZero() {
			for fv.Kind() == reflect.Pointer && !fv.IsNil() {
				fv = fv.Elem()
			}
			if fv.Kind() == reflect.String {
				*secrets = append(*secrets, fv.String())
			}
			*secrets = append(*secrets, fmt.Sprint(fv.Interface()))
		}
	}
}

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
	slices.Sort(ordered)
	ordered = slices.Compact(ordered)
	sort.SliceStable(ordered, func(i, j int) bool { return len(ordered[i]) > len(ordered[j]) })
	replacements := []string{"(redacted)", "(redacted)"}
	for _, s := range ordered {
		replacements = append(replacements, s, "(redacted)")
	}
	return strings.NewReplacer(replacements...).Replace(msg)
}

var secretEscapers = []func(string) string{
	func(s string) string { q := strconv.Quote(s); return q[1 : len(q)-1] },
	func(s string) string { q := strconv.QuoteToASCII(s); return q[1 : len(q)-1] },
	func(s string) string { return jsonBody(s, true) },
	func(s string) string { return jsonBody(s, false) },
	url.QueryEscape,
	url.PathEscape,
}

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

type configField struct {
	index     []int
	key       string
	nested    bool
	path      string
	sf        reflect.StructField
	shadowed  bool
	legacy    string
	hasLegacy bool
}

func (f configField) keyLabel() string {
	if f.nested {
		return f.key + "*"
	}
	return f.key
}

func isNestedStruct(t reflect.Type) bool {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t.Kind() == reflect.Struct && !parsesText(t)
}

var textUnmarshalerType = reflect.TypeFor[encoding.TextUnmarshaler]()

func parsesText(t reflect.Type) bool {
	return reflect.PointerTo(t).Implements(textUnmarshalerType)
}

func configFields(t reflect.Type) ([]configField, error) {
	var all []configField
	if err := collectConfigFields(t, nil, "", "", true, map[reflect.Type]bool{t: true}, &all); err != nil {
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
			} else {
				f.shadowed = true
				out = append(out, f)
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

func collectConfigFields(t reflect.Type, index []int, path, legacy string, legacyOK bool, active map[reflect.Type]bool, out *[]configField) error {
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
				err := collectConfigFields(ft, idx, p, legacy+strings.ToUpper(f.Name)+"_", legacyOK && f.IsExported(), active, out)
				delete(active, ft)
				if err != nil {
					return err
				}
				continue
			}
		}
		if !f.IsExported() {
			continue
		}
		if isNestedStruct(f.Type) {
			key := strings.ToUpper(f.Name) + "_"
			*out = append(*out, configField{index: idx, key: key, nested: true, path: p, sf: f, legacy: legacy + key, hasLegacy: legacyOK})
			continue
		}
		key := f.Tag.Get("config")
		if key == "" {
			key = strings.ToUpper(f.Name)
		}
		*out = append(*out, configField{index: idx, key: key, path: p, sf: f, legacy: legacy + key, hasLegacy: legacyOK})
	}
	return nil
}

func srcHasKey(t reflect.Type, prefix string, src Source) bool {
	fields, err := configFields(t)
	if err != nil {
		return false
	}
	for _, f := range fields {
		if fieldHasKey(f, prefix, src) {
			return true
		}
	}
	return false
}

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

func bindStruct(elem reflect.Value, prefix string, src Source) error {
	fields, err := configFields(elem.Type())
	if err != nil {
		return err
	}
	for _, f := range fields {
		if !f.shadowed && fieldHasKey(f, prefix, src) {
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

		if cf.shadowed {
			if err := applyDefaults(fieldVal, field); err != nil {
				return err
			}
			continue
		}

		if cf.nested {
			nestedPrefix := prefix + cf.key
			if fieldVal.Kind() == reflect.Pointer {
				if fieldVal.IsNil() {
					if !srcHasKey(fieldVal.Type().Elem(), nestedPrefix, src) {
						continue
					}
					fieldVal.Set(reflect.New(fieldVal.Type().Elem()))
				}
				fieldVal = fieldVal.Elem()
			}
			if err := bindStruct(fieldVal, nestedPrefix, src); err != nil {
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

		if val == "" {
			if required {
				return fmt.Errorf("config: required field %s (%s) not set", field.Name, key)
			}
			if err := validateTags(fieldVal, field.Tag.Get("validate")); err != nil {
				return fmt.Errorf("config: field %s: %s", field.Name, err.Error())
			}
			continue
		}

		if err := setField(fieldVal, val, field.Name); err != nil {
			if sensitive {
				return fmt.Errorf("config: field %s: invalid sensitive value (redacted)", field.Name)
			}
			return err
		}
		if err := validateTags(fieldVal, field.Tag.Get("validate")); err != nil {
			return fmt.Errorf("config: field %s: %s", field.Name, err.Error())
		}
	}

	return nil
}

func applyDefaults(v reflect.Value, sf reflect.StructField) error {
	if isNestedStruct(sf.Type) {
		if v.Kind() == reflect.Pointer {
			return nil
		}
		fields, err := configFields(v.Type())
		if err != nil {
			return err
		}
		for _, f := range fields {
			fv, ok := reachField(v, f)
			if !ok || !fv.CanSet() {
				continue
			}
			if err := applyDefaults(fv, f.sf); err != nil {
				return err
			}
		}
		return nil
	}
	def := sf.Tag.Get("default")
	if def == "" {
		return nil
	}
	sensitive := sf.Tag.Get("sensitive") == "true"
	if err := setField(v, def, sf.Name); err != nil {
		if sensitive {
			return fmt.Errorf("config: field %s: invalid sensitive default (redacted)", sf.Name)
		}
		return err
	}
	if err := validateTags(v, sf.Tag.Get("validate")); err != nil {
		return fmt.Errorf("config: field %s: %s", sf.Name, err)
	}
	return nil
}

var legacyWarned sync.Map

type legacyKey struct {
	old, now, path string
}

func warnLegacyKeys(t reflect.Type, src Source) {
	read := map[string]bool{}
	var legacy []legacyKey
	collectLegacyKeys(t, "", "", true, false, read, &legacy, map[reflect.Type]bool{})
	for _, l := range legacy {
		if read[l.old] {
			continue
		}
		if _, ok := src.Get(l.old); !ok {
			continue
		}
		if _, dup := legacyWarned.LoadOrStore(l.old, true); dup {
			continue
		}
		if l.now == "" {
			slog.Warn("config: key is no longer read: the embedded struct field it named is shadowed by an outer field with the same key and takes its default tag",
				"key", l.old, "field", l.path)
			continue
		}
		slog.Warn("config: key is no longer read: embedded struct fields are promoted without the embed's name; set the key this field reads now",
			"key", l.old, "reads", l.now, "field", l.path)
	}
}

func collectLegacyKeys(t reflect.Type, prefix, legacyPrefix string, legacyOK, shadowed bool, read map[string]bool, legacy *[]legacyKey, active map[reflect.Type]bool) {
	if active[t] {
		return
	}
	active[t] = true
	defer delete(active, t)
	fields, err := configFields(t)
	if err != nil {
		return
	}
	for _, f := range fields {
		hidden := shadowed || f.shadowed
		ok := legacyOK && f.hasLegacy
		now, old := prefix+f.key, legacyPrefix+f.legacy
		if f.nested {
			ft := f.sf.Type
			if ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			collectLegacyKeys(ft, now, old, ok, hidden, read, legacy, active)
			continue
		}
		if !hidden {
			read[now] = true
		}
		if ok && old != now {
			l := legacyKey{old: old, now: now, path: f.path}
			if hidden {
				l.now = ""
			}
			*legacy = append(*legacy, l)
		}
	}
}

func setField(v reflect.Value, s string, fieldName string) error {
	if v.Kind() == reflect.Pointer && parsesText(v.Type().Elem()) {
		p := reflect.New(v.Type().Elem())
		if err := setField(p.Elem(), s, fieldName); err != nil {
			return err
		}
		v.Set(p)
		return nil
	}
	if v.CanAddr() && parsesText(v.Type()) {
		if err := v.Addr().Interface().(encoding.TextUnmarshaler).UnmarshalText([]byte(s)); err != nil {
			return fmt.Errorf("config: field %s: %q is not a valid %s: %w", fieldName, s, v.Type(), err)
		}
		return nil
	}
	switch v.Kind() {
	case reflect.String:
		v.SetString(s)
	case reflect.Int, reflect.Int64:
		if v.Type().String() == "time.Duration" {
			d, err := parseDuration(s)
			if err != nil {
				return fmt.Errorf("config: field %s: %q is not a valid duration: %w", fieldName, s, err)
			}
			v.SetInt(int64(d))
			return nil
		}
		return setIntegerField(v, s, fieldName)
	case reflect.Float64:
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return fmt.Errorf("config: field %s: %q is not a valid float: %w", fieldName, s, err)
		}
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

func setIntegerField(value reflect.Value, text, fieldName string) error {
	parsed, err := strconv.ParseInt(text, 10, value.Type().Bits())
	if err != nil {
		return fmt.Errorf("config: field %s: %q is not a valid integer: %w", fieldName, text, err)
	}
	value.SetInt(parsed)
	return nil
}

func parseDuration(s string) (int64, error) {
	if strings.ContainsAny(s, "hmsuµn") {
		d, err := parseGoDuration(s)
		if err == nil {
			return d, nil
		}
		return 0, err
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, err
	}
	const maxSeconds = math.MaxInt64 / int64(time.Second)
	if n > maxSeconds || n < math.MinInt64/int64(time.Second) {
		return 0, fmt.Errorf("duration %ss overflows int64 nanoseconds (max %d seconds)", s, maxSeconds)
	}
	return n * int64(time.Second), nil
}

func parseGoDuration(s string) (int64, error) {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("%w", err)
	}
	return int64(d), nil
}

// MustLoad is Load, panicking on error.
func MustLoad(cfg any, sources ...Source) {
	if err := Load(cfg, sources...); err != nil {
		panic(err)
	}
}

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
