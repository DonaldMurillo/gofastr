package desktop

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/battery/desktop/appstate"
	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// PreferenceKind is one declared preference's value type.
type PreferenceKind string

const (
	PreferenceBool   PreferenceKind = "bool"
	PreferenceInt    PreferenceKind = "int"
	PreferenceString PreferenceKind = "string"
	PreferenceChoice PreferenceKind = "choice"
)

var rePreferenceKey = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// Preference declares one setting.
type Preference struct {
	// Key is the preference's storage and form-field name.
	Key string
	// Label is the field's visible label, in the reader's language.
	Label i18n.Phrase
	// Help is optional field help rendered under the control.
	Help i18n.Phrase
	// Kind is the value type.
	Kind PreferenceKind
	// Default is the value used until something is stored.
	Default any
	// Choices is the option list; required for Choice, refused for every other kind.
	Choices []string
	// Min and Max bound an Int; refused for every other kind.
	Min, Max *int
}

const settingsStateKey = "settings"

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
		if p.Label.IsZero() {
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
			text, ok := p.Default.(string)
			if !ok {
				return fmt.Errorf("desktop: preference %q of kind string needs a string Default", p.Key)
			}
			if strings.ContainsAny(text, "\r\n") {
				return fmt.Errorf("desktop: preference %q string Default must not contain CR or LF line breaks", p.Key)
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

// Preferences is the typed reader and writer over the declared preference list.
type Preferences struct {
	b     *Battery
	decls []Preference
	index map[string]int
	mu    sync.Mutex
}

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

func (p *Preferences) declaration(key string) Preference {
	i, ok := p.index[key]
	if !ok {
		panic(fmt.Sprintf("desktop: preference %q is not declared", key))
	}
	return p.decls[i]
}

// Bool answers the bool preference's stored-or-default value.
func (p *Preferences) Bool(key string) bool {
	d := p.declaration(key)
	if d.Kind != PreferenceBool {
		panic(fmt.Sprintf("desktop: preference %q is of kind %s, not bool", key, d.Kind))
	}
	v, _ := p.value(d).(bool)
	return v
}

// Int answers the int preference's stored-or-default value.
func (p *Preferences) Int(key string) int {
	d := p.declaration(key)
	if d.Kind != PreferenceInt {
		panic(fmt.Sprintf("desktop: preference %q is of kind %s, not int", key, d.Kind))
	}
	v, _ := p.value(d).(int)
	return v
}

// String answers the string or choice preference's stored-or-default value.
func (p *Preferences) String(key string) string {
	d := p.declaration(key)
	if d.Kind != PreferenceString && d.Kind != PreferenceChoice {
		panic(fmt.Sprintf("desktop: preference %q is of kind %s, not string", key, d.Kind))
	}
	v, _ := p.value(d).(string)
	return v
}

// All answers every declared key with its stored-or-default value.
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

func (p *Preferences) warnStored(key string, err error) {
	p.b.logger.Warn("desktop: stored preference falls back to its default",
		"key", key, "error", err.Error())
}

type preferenceError struct {
	key    string
	reason i18n.Phrase
	params map[string]any
}

func (e *preferenceError) Error() string {
	return fmt.Sprintf("preference %q: %s", e.key, e.reason.Format(e.params))
}

func refusePreference(key string, cerr error) *preferenceError {
	if pe, ok := errors.AsType[*i18n.Error](cerr); ok {
		return &preferenceError{key: key, reason: pe.Reason, params: pe.Params}
	}
	return &preferenceError{key: key, reason: reasonPrefBadShape}
}

func (p *Preferences) setRaw(values map[string]json.RawMessage) (keys []string, err error) {
	return p.setValues(values, false)
}

func (p *Preferences) setValues(values map[string]json.RawMessage, form bool) (keys []string, err error) {
	s := p.b.stateStore()
	if s == nil {
		return nil, &Error{Code: CodeUnsupported, Message: "the app state store is not open yet"}
	}
	p.mu.Lock()
	applied, err := p.applyLocked(s, values, form)
	p.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if err := p.b.Emit("preferences_changed", map[string]any{"keys": applied}); err != nil {
		p.b.logger.Warn("desktop: announcing a preferences change failed", "error", err.Error())
	}
	return applied, nil
}

func (p *Preferences) applyLocked(s *appstate.Store, values map[string]json.RawMessage, form bool) ([]string, error) {
	var cur map[string]json.RawMessage
	if _, gerr := s.Get(settingsStateKey, &cur); gerr != nil || cur == nil {
		cur = map[string]json.RawMessage{}
	}
	coerced := make(map[string]any, len(values))
	for _, key := range slices.Sorted(maps.Keys(values)) {
		i, ok := p.index[key]
		if !ok {
			return nil, &preferenceError{key: key, reason: reasonPrefUndeclared}
		}
		v, cerr := coercePreference(p.decls[i], values[key])
		if cerr != nil {
			return nil, refusePreference(key, cerr)
		}
		if p.decls[i].Kind == PreferenceString {
			text := v.(string)
			if strings.ContainsAny(text, "\r\n") {
				return nil, &preferenceError{key: key, reason: reasonPrefLineBreaks}
			}
			var stored string
			if form && json.Unmarshal(cur[key], &stored) == nil && strings.ContainsAny(stored, "\r\n") {
				if text == strings.NewReplacer("\r", "", "\n", "").Replace(stored) {
					continue
				}
				return nil, &preferenceError{key: key, reason: reasonPrefLegacyLines}
			}
		}
		coerced[key] = v
	}

	for key := range coerced {
		delete(cur, key)
	}
	for _, key := range slices.Sorted(maps.Keys(coerced)) {
		data, merr := json.Marshal(coerced[key])
		if merr != nil {
			return nil, &Error{Code: CodeInternal, Message: "encoding a preference failed"}
		}
		cur[key] = data
		encoded, merr := json.Marshal(cur)
		if merr != nil {
			return nil, &Error{Code: CodeInternal, Message: reasonPrefsSaveFailed.Format(nil)}
		}
		if len(encoded) > appstate.MaxValueBytes {
			return nil, &preferenceError{key: key, reason: reasonPrefStorageLimit,
				params: map[string]any{"max": appstate.MaxValueBytes}}
		}
	}
	if serr := s.Set(settingsStateKey, cur); serr != nil {
		return nil, &Error{Code: CodeInternal, Message: "storing preferences failed"}
	}
	return slices.Sorted(maps.Keys(coerced)), nil
}

// Set stores v under the declared key.
func (p *Preferences) Set(key string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return &preferenceError{key: key, reason: reasonPrefBadShape}
	}
	_, err = p.setRaw(map[string]json.RawMessage{key: raw})
	return err
}

func coercePreference(d Preference, raw json.RawMessage) (any, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, i18n.NewError(reasonPrefNull)
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
		return nil, i18n.NewError(reasonPrefNotBool)
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
		return nil, i18n.NewError(reasonPrefNotInt)
	case PreferenceString:
		if !isText {
			return nil, i18n.NewError(reasonPrefNotString)
		}
		return text, nil
	case PreferenceChoice:
		if !isText {
			return nil, i18n.NewError(reasonPrefNotString)
		}
		if !slices.Contains(d.Choices, text) {
			return nil, i18n.NewError(reasonPrefNotChoice, map[string]any{"choices": strings.Join(d.Choices, ", ")})
		}
		return text, nil
	}
	return nil, i18n.NewError(reasonPrefUnknownKind, map[string]any{"kind": strconv.Quote(string(d.Kind))})
}

func checkIntRange(d Preference, n int) (any, error) {
	if d.Min != nil && n < *d.Min {
		return nil, i18n.NewError(reasonPrefTooSmall, map[string]any{"min": *d.Min})
	}
	if d.Max != nil && n > *d.Max {
		return nil, i18n.NewError(reasonPrefTooLarge, map[string]any{"max": *d.Max})
	}
	return n, nil
}
