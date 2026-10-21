package i18n

import (
	"maps"
	"slices"
	"strings"
)

// CatalogProblem names what is wrong with one catalog entry.
type CatalogProblem string

const (
	// ProblemUnknown is a key no [Define] call registered: nothing
	// reads it, so a rename left it behind or a typo hides it.
	ProblemUnknown CatalogProblem = "unknown key"
	// ProblemPlaceholders is a translation naming a {{placeholder}} its
	// English does not fill (it renders as written), or dropping one the
	// English has (the value never shows).
	ProblemPlaceholders CatalogProblem = "placeholders differ"
	// ProblemMissing is a phrase under a [Require]d prefix that the
	// locale does not translate.
	ProblemMissing CatalogProblem = "missing"
)

// CatalogIssue is one place a locale's catalog disagrees with the
// phrases the program defines.
type CatalogIssue struct {
	Locale  string
	Key     string
	Problem CatalogProblem
	// English is the phrase's text, Text the locale's (a plural
	// message's forms joined with " | ").
	English string
	Text    string
}

// CheckOption configures [CheckCatalog].
type CheckOption func(*checkConfig)

type checkConfig struct {
	skip    []string
	require []string
}

// Skip leaves keys under the prefixes alone: the app's own families
// ("entity.", "nav.") that name its data rather than a defined phrase,
// and phrases that are code rather than words.
func Skip(prefixes ...string) CheckOption {
	return func(c *checkConfig) { c.skip = append(c.skip, prefixes...) }
}

// Require reports every phrase under the prefixes whose English holds a
// letter and that the locale lacks. Without it a partial catalog is
// fine: a miss renders the English.
func Require(prefixes ...string) CheckOption {
	return func(c *checkConfig) { c.require = append(c.require, prefixes...) }
}

// CheckCatalog compares locale's entries in c with every phrase the
// program defined ([Phrases]) and returns the disagreements, sorted by
// key. Run it in a test for each locale an app ships and fail on a
// non-empty result: a catalog slip is otherwise invisible, one English
// sentence among translated ones.
//
// Only phrases linked into the binary are known, so run it from a test
// in the package that wires the whole app (its main package).
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

// placeholdersAgree reports whether m fills exactly the English's
// placeholders. A plain translation names the same set. A plural one may
// leave the count out of a form ("one" reads "a result"), but every form
// keeps the English's other placeholders and names nothing it lacks, and
// the forms together name them all.
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

// isCountParam reports whether name is a param plural selection reads
// its count from (see extractCount).
func isCountParam(name string) bool {
	return slices.Contains(countParams, name)
}

// Placeholders returns the distinct {{name}} placeholder names in s,
// sorted: the set a translation of s must keep. A name is taken as
// written, spaces included: the runtime's say() and the headless fills
// match {{name}} exactly, so "{{ name }}" is a different placeholder.
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
