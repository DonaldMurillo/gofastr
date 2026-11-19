package i18n

import (
	"maps"
	"slices"
	"strings"
)

// CatalogProblem names what is wrong with one catalog entry.
type CatalogProblem string

const (
	// ProblemUnknown is a key no [Define] call registered.
	ProblemUnknown CatalogProblem = "unknown key"
	// ProblemPlaceholders is a translation whose placeholders differ from the English.
	ProblemPlaceholders CatalogProblem = "placeholders differ"
	// ProblemMissing is a [Require]d phrase the locale does not translate.
	ProblemMissing CatalogProblem = "missing"
	// ProblemNoOther is a plural translation with no "other" form.
	ProblemNoOther CatalogProblem = "plural without other"
)

// CatalogIssue is one entry where a catalog disagrees with the defined phrases.
type CatalogIssue struct {
	Locale  string
	Key     string
	Problem CatalogProblem
	// English is the phrase's text, Text the locale's (plural forms joined by " | ").
	English string
	Text    string
}

// CheckOption configures [CheckCatalog].
type CheckOption func(*checkConfig)

type checkConfig struct {
	skip    []string
	require []string
}

// Skip leaves keys under the prefixes unchecked.
func Skip(prefixes ...string) CheckOption {
	return func(c *checkConfig) { c.skip = append(c.skip, prefixes...) }
}

// Require reports every lettered phrase under the prefixes the locale lacks.
func Require(prefixes ...string) CheckOption {
	return func(c *checkConfig) { c.require = append(c.require, prefixes...) }
}

// CheckCatalog returns, sorted by key, where locale's entries in c disagree
// with the phrases linked into the binary ([Phrases]).
func CheckCatalog(c *MapCatalog, locale string, opts ...CheckOption) []CatalogIssue {
	var cfg checkConfig
	for _, o := range opts {
		o(&cfg)
	}
	has := func(key string, prefixes []string) bool {
		for _, p := range prefixes {
			if strings.HasPrefix(key, p) {
				return true
			}
		}
		return false
	}
	phrases := Phrases()
	var entries map[string]Message
	if c != nil {
		c.mu.RLock()
		entries = maps.Clone(c.Entries[normalize(locale)])
		c.mu.RUnlock()
	}
	var out []CatalogIssue
	for _, key := range slices.Sorted(maps.Keys(entries)) {
		if has(key, cfg.skip) {
			continue
		}
		m := entries[key]
		text := m.Text
		if len(m.Plural) > 0 {
			forms := make([]string, 0, len(m.Plural))
			for _, k := range slices.Sorted(maps.Keys(m.Plural)) {
				forms = append(forms, m.Plural[k])
			}
			text = strings.Join(forms, " | ")
		}
		en, ok := phrases[key]
		if !ok {
			out = append(out, CatalogIssue{Locale: locale, Key: key, Problem: ProblemUnknown, Text: text})
			continue
		}
		if _, other := m.Plural["other"]; len(m.Plural) > 0 && !other {
			out = append(out, CatalogIssue{Locale: locale, Key: key, Problem: ProblemNoOther, English: en, Text: text})
		}
		if !placeholdersAgree(en, m) {
			out = append(out, CatalogIssue{Locale: locale, Key: key, Problem: ProblemPlaceholders, English: en, Text: text})
		}
	}
	for _, key := range slices.Sorted(maps.Keys(phrases)) {
		if _, ok := entries[key]; ok || !has(key, cfg.require) || has(key, cfg.skip) {
			continue
		}
		if en := phrases[key]; strings.IndexFunc(en, isLetter) >= 0 {
			out = append(out, CatalogIssue{Locale: locale, Key: key, Problem: ProblemMissing, English: en})
		}
	}
	slices.SortStableFunc(out, func(a, b CatalogIssue) int { return strings.Compare(a.Key, b.Key) })
	return out
}

func isLetter(r rune) bool { return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' }

func placeholdersAgree(english string, m Message) bool {
	want := Placeholders(english)
	if len(m.Plural) == 0 {
		return slices.Equal(want, Placeholders(m.Text))
	}
	var union []string
	for _, form := range m.Plural {
		got := Placeholders(form)
		for _, p := range got {
			if !slices.Contains(want, p) {
				return false
			}
		}
		for _, p := range want {
			if !isCountParam(p) && !slices.Contains(got, p) {
				return false
			}
		}
		union = append(union, got...)
	}
	slices.Sort(union)
	return slices.Equal(want, slices.Compact(union))
}

func isCountParam(name string) bool {
	return slices.Contains(countParams, name)
}

// Placeholders returns the distinct {{name}} names in s, sorted, taken as
// written.
func Placeholders(s string) []string {
	var out []string
	for {
		open := strings.Index(s, "{{")
		if open < 0 {
			break
		}
		end := strings.Index(s[open+2:], "}}")
		if end < 0 {
			break
		}
		out = append(out, s[open+2:open+2+end])
		s = s[open+2+end+2:]
	}
	slices.Sort(out)
	return slices.Compact(out)
}
