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
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"
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
// Conflating those breaks defaulting — an explicit empty value used to
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
// Validate() error method — a config struct that doubled as an
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
// the parent field name (e.g. `DB DBConfig` reads keys like DB_HOST).
//
// Fields tagged `sensitive:"true"` have their raw value redacted from
// any error messages.
//
// If the config (or a pointer to it) implements ConfigValidator,
// Validate is called after binding and its error is returned.
//
// Supported field types: string, int, int64, float64, bool, Duration,
// and nested structs.
func Load(cfg interface{}, sources ...Source) error {
	return LoadWith(cfg, sources...)
}

// LoadWith is an alias for Load. Populates config from sources.
func LoadWith(cfg interface{}, sources ...Source) error {
	v := reflect.ValueOf(cfg)
	if v.Kind() != reflect.Ptr || v.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("config: expected pointer to struct, got %T", cfg)
	}

	src := ChainedSource(sources)
	if len(sources) == 0 {
		src = ChainedSource{EnvSource{}}
	}

	if err := bindStruct(v.Elem(), "", src); err != nil {
		return err
	}

	if val, ok := cfg.(ConfigValidator); ok {
		if err := val.Validate(); err != nil {
			return fmt.Errorf("config: validate: %w", err)
		}
	}
	return nil
}

// bindStruct walks the fields of elem, reading from src under the given
// prefix. Nested struct fields recurse with prefix+FieldName+"_".
func bindStruct(elem reflect.Value, prefix string, src Source) error {
	t := elem.Type()

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		fieldVal := elem.Field(i)

		if !fieldVal.CanSet() {
			continue
		}

		// Recurse into nested structs (but not time.Duration, which is an int64).
		if fieldVal.Kind() == reflect.Struct && fieldVal.Type() != reflect.TypeOf(time.Duration(0)) {
			nestedPrefix := prefix + strings.ToUpper(field.Name) + "_"
			if err := bindStruct(fieldVal, nestedPrefix, src); err != nil {
				return err
			}
			continue
		}

		key := field.Tag.Get("config")
		if key == "" {
			key = strings.ToUpper(field.Name)
		}
		key = prefix + key

		required := field.Tag.Get("required") == "true"
		defaultVal := field.Tag.Get("default")
		sensitive := field.Tag.Get("sensitive") == "true"

		val, found := src.Get(key)
		if !found {
			if required && defaultVal == "" {
				return fmt.Errorf("config: required field %s (%s) not set", field.Name, key)
			}
			val = defaultVal
		}

		if val == "" && !required {
			continue // leave zero value
		}

		if err := setField(fieldVal, val, field.Name); err != nil {
			if sensitive {
				return fmt.Errorf("config: field %s: invalid sensitive value (redacted)", field.Name)
			}
			return err
		}
	}

	return nil
}

// setField sets a reflect.Value from a string, converting to the
// appropriate type.
func setField(v reflect.Value, s string, fieldName string) error {
	if s == "" {
		return nil
	}

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
	return n * int64(1e9), nil
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
func MustLoad(cfg interface{}, sources ...Source) {
	if err := Load(cfg, sources...); err != nil {
		panic(err)
	}
}
