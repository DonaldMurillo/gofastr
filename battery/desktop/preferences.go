package desktop

import (
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/battery/desktop/appstate"
)

// Declared preferences: typed app settings on Config.Preferences, stored
// in the app state under "settings", read back typed through
// (*Battery).Preferences, and rendered by PreferencesScreen (the screen
// builder the host mounts). The declaration is the whole surface: a key
// nobody declared cannot be stored, read, or rendered.
//
// EXPERIMENTAL: part of battery/desktop.

// PreferenceKind is one declared preference's value type.
type PreferenceKind string

const (
	PreferenceBool   PreferenceKind = "bool"
	PreferenceInt    PreferenceKind = "int"
	PreferenceString PreferenceKind = "string"
	PreferenceChoice PreferenceKind = "choice"
)

// rePreferenceKey is the key grammar: a lowercase letter, then up to 63
// lowercase letters, digits, or underscores. Dots would collide with the
// app state's namespace separators, so they are refused.
var rePreferenceKey = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// Preference declares one setting. Default must match Kind (bool, int,
// string, and for Choice a string listed in Choices); Min and Max bound
// an Int (both optional, but Min must not exceed Max); Choices is the
// option list of a Choice. New panics on a declaration that breaks any
// of that: it is a wiring error, the same posture as a bad menu.
type Preference struct {
	// Key is the preference's storage and form-field name. It must
	// match ^[a-z][a-z0-9_]{0,63}$ and be unique in the list.
	Key string
	// Label is the field's visible label. Required.
	Label string
	// Help is optional field help rendered under the control.
	Help string
	// Kind is the value type. Required (the zero value is refused).
	Kind PreferenceKind
	// Default is the value used until something is stored. It must
	// match Kind: bool, int, string, or (Choice) a string in Choices.
	Default any
	// Choices is the option list; required for Choice, refused for
	// every other kind.
	Choices []string
	// Min and Max bound an Int; refused for every other kind.
	Min, Max *int
}

// settingsStateKey is the app state key holding every stored
// preference: one JSON object mapping preference keys to their typed
// values, e.g. {"work_minutes":25,"sound":"chime"}. The state
// capability confines page keys to "page.", so this entry is never
// reachable from the page.
const settingsStateKey = "settings"

// validatePreferences refuses a declaration list New would otherwise
// have to debug at first use. Every rule names the key that broke it.
func validatePreferences(decls []Preference) error {
	seen := make(map[string]struct{}, len(decls))
	for _, p := range decls {
		if !rePreferenceKey.MatchString(p.Key) {
			return fmt.Errorf("desktop: preference key %q must match ^[a-z][a-z0-9_]{0,63}$", p.Key)
		}
		if _, dup := seen[p.Key]; dup {
			return fmt.Errorf("desktop: duplicate preference key %q", p.Key)
		}
		seen[p.Key] = struct{}{}
		if p.Label == "" {
			return fmt.Errorf("desktop: preference %q requires Label", p.Key)
		}
		if p.Kind != PreferenceInt && (p.Min != nil || p.Max != nil) {
			return fmt.Errorf("desktop: preference %q of kind %s cannot carry Min or Max (int only)", p.Key, p.Kind)
		}
		if p.Kind != PreferenceChoice && len(p.Choices) > 0 {
			return fmt.Errorf("desktop: preference %q of kind %s cannot carry Choices (choice only)", p.Key, p.Kind)
		}
		switch p.Kind {
		case PreferenceBool:
			if _, ok := p.Default.(bool); !ok {
				return fmt.Errorf("desktop: preference %q of kind bool needs a bool Default", p.Key)
			}
		case PreferenceInt:
			if _, ok := p.Default.(int); !ok {
				return fmt.Errorf("desktop: preference %q of kind int needs an int Default", p.Key)
			}
			if p.Min != nil && p.Max != nil && *p.Min > *p.Max {
				return fmt.Errorf("desktop: preference %q has Min %d greater than Max %d", p.Key, *p.Min, *p.Max)
			}
			if p.Min != nil {
				if def, ok := p.Default.(int); ok && def < *p.Min {
					return fmt.Errorf("desktop: preference %q has Default %d below Min %d", p.Key, def, *p.Min)
				}
			}
			if p.Max != nil {
				if def, ok := p.Default.(int); ok && def > *p.Max {
					return fmt.Errorf("desktop: preference %q has Default %d above Max %d", p.Key, def, *p.Max)
				}
			}
		case PreferenceString:
			if _, ok := p.Default.(string); !ok {
				return fmt.Errorf("desktop: preference %q of kind string needs a string Default", p.Key)
			}
		case PreferenceChoice:
			if len(p.Choices) == 0 {
				return fmt.Errorf("desktop: preference %q of kind choice requires Choices", p.Key)
			}
			def, ok := p.Default.(string)
			if !ok {
				return fmt.Errorf("desktop: preference %q of kind choice needs a string Default", p.Key)
			}
			if !slices.Contains(p.Choices, def) {
				return fmt.Errorf("desktop: preference %q has Default %q, which is not one of Choices", p.Key, def)
			}
		default:
			return fmt.Errorf("desktop: preference %q has unknown kind %q (bool, int, string, choice)", p.Key, p.Kind)
		}
	}
	return nil
}

// Preferences is the typed reader and writer over the declared
// preference list. Get it from (*Battery).Preferences; it reads the app
// state store once Run has opened it and answers the declared defaults
// before that (the --serve shape). Safe for concurrent use.
type Preferences struct {
	b     *Battery
	decls []Preference
	index map[string]int
	// mu serializes the read-modify-write of the one settings entry,
	// the same job windowStore.mu does for the remembered windows.
	mu sync.Mutex
}

// newPreferences validates decls and takes a defensive copy (Choices,
// Min, Max included) so a caller mutating its Config afterwards cannot
// change what the battery serves.
func newPreferences(b *Battery, decls []Preference) (*Preferences, error) {
	if err := validatePreferences(decls); err != nil {
		return nil, err
	}
	copies := make([]Preference, len(decls))
	index := make(map[string]int, len(decls))
	for i, p := range decls {
		c := p
		c.Choices = slices.Clone(p.Choices)
		if p.Min != nil {
			v := *p.Min
			c.Min = &v
		}
		if p.Max != nil {
			v := *p.Max
			c.Max = &v
		}
		copies[i] = c
		index[p.Key] = i
	}
	return &Preferences{b: b, decls: copies, index: index}, nil
}

// Declared returns the validated declarations, in declaration order.
func (p *Preferences) Declared() []Preference {
	return slices.Clone(p.decls)
}

// declaration panics on an undeclared key: like a bad Config, that is a
// programming error the caller wants loudly, not an error to handle.
func (p *Preferences) declaration(key string) Preference {
	i, ok := p.index[key]
	if !ok {
		panic(fmt.Sprintf("desktop: preference %q is not declared", key))
	}
	return p.decls[i]
}

// Bool answers the bool preference's stored-or-default value. It panics
// when the key is undeclared or declared as another kind.
func (p *Preferences) Bool(key string) bool {
	d := p.declaration(key)
	if d.Kind != PreferenceBool {
		panic(fmt.Sprintf("desktop: preference %q is of kind %s, not bool", key, d.Kind))
	}
	v, _ := p.value(d).(bool)
	return v
}

// Int answers the int preference's stored-or-default value. It panics
// when the key is undeclared or declared as another kind.
func (p *Preferences) Int(key string) int {
	d := p.declaration(key)
	if d.Kind != PreferenceInt {
		panic(fmt.Sprintf("desktop: preference %q is of kind %s, not int", key, d.Kind))
	}
	v, _ := p.value(d).(int)
	return v
}

// String answers the string or choice preference's stored-or-default
// value. It panics when the key is undeclared or declared as another
// kind.
func (p *Preferences) String(key string) string {
	d := p.declaration(key)
	if d.Kind != PreferenceString && d.Kind != PreferenceChoice {
		panic(fmt.Sprintf("desktop: preference %q is of kind %s, not string", key, d.Kind))
	}
	v, _ := p.value(d).(string)
	return v
}

// All answers every declared key with its stored-or-default value.
// Before Run (no store) that is the declared defaults, which is also
// what an app serving itself over plain HTTP behind --serve reads.
func (p *Preferences) All() map[string]any {
	stored := p.storedMap()
	out := make(map[string]any, len(p.decls))
	for _, d := range p.decls {
		out[d.Key] = d.Default
		if raw, ok := stored[d.Key]; ok {
			if v, err := coercePreference(d, raw); err == nil {
				out[d.Key] = v
			} else {
				p.warnStored(d.Key, err)
			}
		}
	}
	return out
}

// value answers one declaration's stored-or-default value. The type
// assertion is split from its use because coercePreference guarantees
// the kind's Go type on success; on failure the default (validated at
// New to match the kind) is the answer.
func (p *Preferences) value(d Preference) any {
	if raw, ok := p.storedMap()[d.Key]; ok {
		if v, err := coercePreference(d, raw); err == nil {
			return v
		} else {
			p.warnStored(d.Key, err)
		}
	}
	return d.Default
}

// storedMap returns the stored settings entry. Nil (an empty read)
// when there is no store yet, nothing is stored, or the entry does not
// decode: every one of those means "declared defaults", the store's own
// posture for a truncated quit-time write.
func (p *Preferences) storedMap() map[string]json.RawMessage {
	s := p.b.stateStore()
	if s == nil {
		return nil
	}
	var m map[string]json.RawMessage
	if _, err := s.Get(settingsStateKey, &m); err != nil {
		p.b.logger.Warn("desktop: the stored preferences do not decode; using declared defaults",
			"error", err.Error())
		return nil
	}
	return m
}

// warnStored reports one stored value that failed its own declaration.
func (p *Preferences) warnStored(key string, err error) {
	p.b.logger.Warn("desktop: stored preference falls back to its default",
		"key", key, "error", err.Error())
}

// preferenceError is one invalid value for one named preference. The
// capability answers it as invalid_input and the preferences screen's
// route turns it into the form-error envelope for that field.
type preferenceError struct {
	key string
	msg string
}

func (e *preferenceError) Error() string {
	return fmt.Sprintf("preference %q: %s", e.key, e.msg)
}

// setRaw applies every entry at once: all entries are validated first
// and the first invalid one refuses the whole call (a half-applied
// settings form is the bug this prevents), then the merged map is
// stored under settingsStateKey and preferences_changed carries the
// applied keys to every open window. keys is sorted.
func (p *Preferences) setRaw(values map[string]json.RawMessage) (keys []string, err error) {
	s := p.b.stateStore()
	if s == nil {
		return nil, &Error{Code: CodeUnsupported, Message: "the app state store is not open yet"}
	}
	p.mu.Lock()
	applied, err := p.applyLocked(s, values)
	p.mu.Unlock()
	if err != nil {
		return nil, err
	}
	// Announce after the write and outside the lock: Emit evaluates
	// JavaScript in every window and must never run under a mutex.
	if err := p.b.Emit("preferences_changed", map[string]any{"keys": applied}); err != nil {
		p.b.logger.Warn("desktop: announcing a preferences change failed", "error", err.Error())
	}
	return applied, nil
}

// applyLocked validates every entry and merges it into the stored
// settings entry; the caller holds mu so two writers cannot lose one's
// keys to the other's read-modify-write.
func (p *Preferences) applyLocked(s *appstate.Store, values map[string]json.RawMessage) ([]string, error) {
	coerced := make(map[string]any, len(values))
	for _, key := range slices.Sorted(maps.Keys(values)) {
		i, ok := p.index[key]
		if !ok {
			return nil, &preferenceError{key: key, msg: "not a declared preference"}
		}
		v, cerr := coercePreference(p.decls[i], values[key])
		if cerr != nil {
			return nil, &preferenceError{key: key, msg: cerr.Error()}
		}
		coerced[key] = v
	}

	// Read-modify-write of the one settings entry: an undeclared key
	// already in the file (an app that removed a preference) is left
	// in place and simply never read.
	var cur map[string]json.RawMessage
	if _, gerr := s.Get(settingsStateKey, &cur); gerr != nil {
		cur = map[string]json.RawMessage{}
	}
	if cur == nil {
		cur = map[string]json.RawMessage{}
	}
	for _, key := range slices.Sorted(maps.Keys(coerced)) {
		data, merr := json.Marshal(coerced[key])
		if merr != nil {
			// Unreachable: coerced values are bool, int, or string.
			return nil, &Error{Code: CodeInternal, Message: "encoding a preference failed"}
		}
		cur[key] = data
	}
	if serr := s.Set(settingsStateKey, cur); serr != nil {
		return nil, &Error{Code: CodeInternal, Message: "storing preferences failed"}
	}
	return slices.Sorted(maps.Keys(coerced)), nil
}

// Set stores v under the declared key. The accepted shapes are the
// JSON forms of the kind's Go type (bool, whole number, string) plus
// the string spellings the runtime's form serializer produces ("true",
// "30"): app code passes the typed value, the form path passes the
// string. Range and choice rules apply; a refusal is a *preferenceError
// and before Run (no store) it is *Error CodeUnsupported.
func (p *Preferences) Set(key string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return &preferenceError{key: key, msg: "must be a bool, a whole number, or a string"}
	}
	_, err = p.setRaw(map[string]json.RawMessage{key: raw})
	return err
}

// coercePreference validates one raw JSON value against a declaration
// and answers its Go value. Accepted forms: the kind's native JSON type
// (true, 25, "chime") and, for bool and int, the string spellings the
// form serializer sends ("true", "30"). A blank int string is refused
// here; the form route skips blank ints before calling (they mean "not
// provided", and only that path knows it).
func coercePreference(d Preference, raw json.RawMessage) (any, error) {
	// JSON null unmarshals into any type with a nil error and leaves
	// the zero value behind, so every branch below would take it for a
	// valid value of its kind. No rendered control produces it; refuse
	// it before the kind branches, which also covers the stored-value
	// read path (value() coerces what it reads back).
	if len(raw) == 0 || string(raw) == "null" {
		return nil, fmt.Errorf("must not be null")
	}
	var text string
	isText := json.Unmarshal(raw, &text) == nil
	switch d.Kind {
	case PreferenceBool:
		var b bool
		if json.Unmarshal(raw, &b) == nil {
			return b, nil
		}
		if isText && (text == "true" || text == "false") {
			return text == "true", nil
		}
		return nil, fmt.Errorf("must be true or false")
	case PreferenceInt:
		var n int
		if json.Unmarshal(raw, &n) == nil {
			return checkIntRange(d, n)
		}
		if isText && text != "" {
			if p, err := strconv.Atoi(text); err == nil {
				return checkIntRange(d, p)
			}
		}
		return nil, fmt.Errorf("must be a whole number")
	case PreferenceString:
		if !isText {
			return nil, fmt.Errorf("must be a string")
		}
		return text, nil
	case PreferenceChoice:
		if !isText {
			return nil, fmt.Errorf("must be a string")
		}
		if !slices.Contains(d.Choices, text) {
			return nil, fmt.Errorf("must be one of: %s", strings.Join(d.Choices, ", "))
		}
		return text, nil
	}
	return nil, fmt.Errorf("unknown kind %q", d.Kind)
}

// checkIntRange applies an int declaration's Min and Max.
func checkIntRange(d Preference, n int) (any, error) {
	if d.Min != nil && n < *d.Min {
		return nil, fmt.Errorf("must be at least %d", *d.Min)
	}
	if d.Max != nil && n > *d.Max {
		return nil, fmt.Errorf("must be at most %d", *d.Max)
	}
	return n, nil
}
