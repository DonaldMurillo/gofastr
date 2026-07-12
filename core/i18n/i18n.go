// Package i18n is a small internationalization primitive for GoFastr
// apps: locale negotiation from `Accept-Language`, JSON-backed message
// catalogs with `{{placeholder}}` interpolation, and CLDR-style plural
// categories (zero / one / two / few / many / other) with sensible
// English defaults and a hook for per-locale custom rules.
//
// The goal is to make "translate this string for this caller's locale"
// trivial without pulling in the full ICU stack. Number / date /
// currency formatting is out of scope here; use stdlib `time` /
// `strconv` or wire your own formatter on top.
//
// Wiring:
//
//	cat, _ := i18n.LoadJSONCatalog(os.DirFS("locales"), ".")
//	tr := i18n.NewTranslator(cat, "en")
//	i18n.SetDefault(tr)
//
//	// then in a handler
//	msg := i18n.T(r.Context(), "cart.items", map[string]any{"count": 3})
//
// Wire the [Middleware] into your router so per-request locale lookup
// happens from `Accept-Language` (or a custom resolver) before
// handlers fire.
package i18n

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Locale is a BCP 47 language tag (lower-cased base + optional region:
// "en", "en-us", "fr-ca"). Empty Tag means "no preference, fall back."
type Locale struct {
	Tag string
}

// String renders the tag — useful in templates that already know they
// have a Locale.
func (l Locale) String() string { return l.Tag }

// Message is one entry in a catalog. Either Text is set (the simple
// case) or Plural is non-empty (a CLDR-categorised set of variants).
type Message struct {
	Text   string
	Plural map[string]string
}

// Catalog is the read-side interface for message lookups. A nil
// Catalog (or one that always returns ok=false) makes the Translator
// fall back to the bare key — useful while bootstrapping a new app.
type Catalog interface {
	Get(locale, key string) (Message, bool)
	Locales() []string
}

// PluralRule maps an integer count to a CLDR plural category for the
// locale. Built-in rules cover English ("one" if n==1, "other"
// otherwise); register more with [Translator.RegisterPluralRule] or
// drop in a third-party CLDR-data set.
type PluralRule func(n int) string

// Translator looks up keys in a Catalog, applies per-locale plural
// rules, and interpolates {{placeholders}} from params.
type Translator struct {
	catalog  Catalog
	fallback string

	mu    sync.RWMutex
	rules map[string]PluralRule
}

// NewTranslator wraps a Catalog with a fallback locale (used when the
// request locale is missing entries). Pass an empty fallback to
// disable fallback (missing keys return the bare key).
func NewTranslator(c Catalog, fallback string) *Translator {
	return &Translator{
		catalog:  c,
		fallback: normalize(fallback),
		rules: map[string]PluralRule{
			"en": englishPlural,
		},
	}
}

// RegisterPluralRule installs a plural rule for the supplied locale.
// Looked up first by exact tag, then by language base (so "en-US"
// falls back to "en").
func (t *Translator) RegisterPluralRule(locale string, rule PluralRule) *Translator {
	t.mu.Lock()
	t.rules[normalize(locale)] = rule
	t.mu.Unlock()
	return t
}

// T returns the translated message for key, interpolating any
// {{placeholder}} tokens from the first params map. When the message
// is a plural and params contains a numeric "count" / "n", the
// matching category is selected via the locale's PluralRule.
//
// Lookup order: ctx locale → fallback locale → bare key. Empty key
// returns empty string.
func (t *Translator) T(ctx context.Context, key string, params ...map[string]any) string {
	if t == nil || key == "" {
		return key
	}
	p := mergeParams(params)
	tag := FromContext(ctx).Tag
	if msg, ok := t.lookup(tag, key); ok {
		return t.render(tag, msg, p)
	}
	if t.fallback != "" {
		if msg, ok := t.lookup(t.fallback, key); ok {
			return t.render(t.fallback, msg, p)
		}
	}
	return key
}

// lookup walks the catalog with progressive language tag truncation
// (so "fr-CA" falls back to "fr" before missing entirely).
func (t *Translator) lookup(tag, key string) (Message, bool) {
	if t.catalog == nil {
		return Message{}, false
	}
	for _, candidate := range tagFallbacks(tag) {
		if m, ok := t.catalog.Get(candidate, key); ok {
			return m, true
		}
	}
	return Message{}, false
}

// render interpolates placeholders and applies plural selection.
func (t *Translator) render(tag string, m Message, params map[string]any) string {
	if len(m.Plural) > 0 {
		n, ok := extractCount(params)
		if !ok {
			// No count supplied — pick the "other" variant if present;
			// otherwise fall through to interpolate whatever Text was.
			if v, has := m.Plural["other"]; has {
				return interpolate(v, params)
			}
			return interpolate(m.Text, params)
		}
		cat := t.pluralCategory(tag, n)
		if v, has := m.Plural[cat]; has {
			return interpolate(v, params)
		}
		if v, has := m.Plural["other"]; has {
			return interpolate(v, params)
		}
	}
	return interpolate(m.Text, params)
}

func (t *Translator) pluralCategory(tag string, n int) string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	for _, c := range tagFallbacks(tag) {
		if r, ok := t.rules[c]; ok {
			return r(n)
		}
	}
	return englishPlural(n)
}

// englishPlural is the conservative default: "one" iff n == 1, else
// "other". Reasonable for English-ish languages; register a real rule
// for languages with more categories (Arabic, Russian, etc.).
func englishPlural(n int) string {
	if n == 1 {
		return "one"
	}
	return "other"
}

// ----- catalog implementations ---------------------------------------------

// MapCatalog is the simplest in-memory Catalog — useful in tests and
// for embedding small string sets directly in code.
type MapCatalog struct {
	Entries map[string]map[string]Message // locale → key → message
}

// NewMapCatalog returns an empty MapCatalog.
func NewMapCatalog() *MapCatalog {
	return &MapCatalog{Entries: map[string]map[string]Message{}}
}

// Set writes a message into the catalog. Convenience for tests.
func (c *MapCatalog) Set(locale, key string, m Message) {
	tag := normalize(locale)
	if c.Entries[tag] == nil {
		c.Entries[tag] = map[string]Message{}
	}
	c.Entries[tag][key] = m
}

// Get implements Catalog.
func (c *MapCatalog) Get(locale, key string) (Message, bool) {
	if c == nil {
		return Message{}, false
	}
	if m, ok := c.Entries[normalize(locale)][key]; ok {
		return m, true
	}
	return Message{}, false
}

// Locales implements Catalog.
func (c *MapCatalog) Locales() []string {
	out := make([]string, 0, len(c.Entries))
	for k := range c.Entries {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// LoadJSONCatalog reads `<locale>.json` files from dir on the provided
// fs.FS. Each file is expected to map keys to either strings (Text) or
// objects whose keys are plural categories. Nested keys are flattened
// with `.` separators so `{"cart": {"items": ...}}` becomes `cart.items`.
//
// Example file (en.json):
//
//	{
//	  "welcome": "Hello, {{name}}!",
//	  "cart.items": { "one": "1 item", "other": "{{count}} items" }
//	}
func LoadJSONCatalog(fsys fs.FS, dir string) (*MapCatalog, error) {
	if fsys == nil {
		return nil, errors.New("i18n: nil FS")
	}
	if dir == "" {
		dir = "."
	}
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("i18n: read dir %q: %w", dir, err)
	}
	cat := NewMapCatalog()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		locale := normalize(strings.TrimSuffix(e.Name(), ".json"))
		raw, err := fs.ReadFile(fsys, filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("i18n: read %s: %w", e.Name(), err)
		}
		var doc map[string]any
		if err := json.Unmarshal(raw, &doc); err != nil {
			return nil, fmt.Errorf("i18n: parse %s: %w", e.Name(), err)
		}
		flat := map[string]Message{}
		flatten("", doc, flat)
		cat.Entries[locale] = flat
	}
	return cat, nil
}

// flatten walks the parsed JSON tree producing a flat key→Message map.
// Nested objects whose keys are all plural categories become a plural
// Message; everything else is a Text Message.
func flatten(prefix string, node map[string]any, out map[string]Message) {
	for k, v := range node {
		full := k
		if prefix != "" {
			full = prefix + "." + k
		}
		switch val := v.(type) {
		case string:
			out[full] = Message{Text: val}
		case map[string]any:
			if isPluralBucket(val) {
				p := make(map[string]string, len(val))
				for cat, raw := range val {
					if s, ok := raw.(string); ok {
						p[cat] = s
					}
				}
				out[full] = Message{Plural: p}
			} else {
				flatten(full, val, out)
			}
		}
	}
}

var pluralCategories = map[string]struct{}{
	"zero": {}, "one": {}, "two": {}, "few": {}, "many": {}, "other": {},
}

// isPluralBucket reports whether every key is a CLDR plural category;
// only then do we treat the object as a Plural message rather than a
// nested namespace.
func isPluralBucket(m map[string]any) bool {
	if len(m) == 0 {
		return false
	}
	for k, v := range m {
		if _, ok := pluralCategories[k]; !ok {
			return false
		}
		if _, ok := v.(string); !ok {
			return false
		}
	}
	return true
}

// ----- context plumbing -----------------------------------------------------

type ctxKey struct{}

// WithContext attaches a Locale to ctx. Middleware should call this
// once per request after resolving the caller's preferred language.
func WithContext(ctx context.Context, l Locale) context.Context {
	return context.WithValue(ctx, ctxKey{}, l)
}

// FromContext returns the attached Locale, or a zero value if none.
func FromContext(ctx context.Context) Locale {
	if ctx == nil {
		return Locale{}
	}
	if v, ok := ctx.Value(ctxKey{}).(Locale); ok {
		return v
	}
	return Locale{}
}

// ----- default Translator (package-level helpers) --------------------------

var (
	defaultMu sync.RWMutex
	def       *Translator
)

// SetDefault installs the process-wide Translator used by package-
// level helpers ([T]). Pass nil to disable.
func SetDefault(t *Translator) {
	defaultMu.Lock()
	def = t
	defaultMu.Unlock()
}

// Default returns the installed Translator, or nil if none.
func Default() *Translator {
	defaultMu.RLock()
	defer defaultMu.RUnlock()
	return def
}

// T is a convenience wrapper around Default().T.
func T(ctx context.Context, key string, params ...map[string]any) string {
	d := Default()
	if d == nil {
		return key
	}
	return d.T(ctx, key, params...)
}

// ----- middleware ----------------------------------------------------------

// NegotiateOption configures locale negotiation (see [Negotiate] and
// [Middleware]).
type NegotiateOption func(*negotiateConfig)

type negotiateConfig struct {
	resolvers []func(*http.Request) (string, bool)
}

// WithLocaleResolver installs a custom locale resolver consulted BEFORE
// the X-Locale / Accept-Language headers. The resolver returns a BCP 47
// tag and ok=true when it has a preference (e.g. a stored per-user
// locale cookie); ok=false falls through to header-based negotiation.
//
// The returned tag is matched against the catalog's locales (with the
// same tag fallbacks as Accept-Language: "fr-ca" → "fr"). An unknown,
// malformed, or over-long value falls through to the next source
// rather than producing an unmatched locale — resolver values are
// attacker-controlled (cookies), so they are length- and
// character-bounded before any matching.
func WithLocaleResolver(f func(*http.Request) (string, bool)) NegotiateOption {
	return func(c *negotiateConfig) {
		if f != nil {
			c.resolvers = append(c.resolvers, f)
		}
	}
}

// CookieLocale returns a resolver that reads the caller's preferred
// locale from the named cookie. A missing or empty cookie returns
// ok=false so negotiation falls through to the headers. Pair with
// [WithLocaleResolver] and a set-cookie handler for per-user locale
// switching.
func CookieLocale(name string) func(*http.Request) (string, bool) {
	return func(r *http.Request) (string, bool) {
		if r == nil || name == "" {
			return "", false
		}
		c, err := r.Cookie(name)
		if err != nil {
			return "", false
		}
		return c.Value, c.Value != ""
	}
}

// maxResolverTagLen bounds how long an attacker-controlled resolver
// value (cookie) may be before it is rejected. Mirrors the Accept-
// Language bounding: real BCP 47 tags are well under this.
const maxResolverTagLen = 35

// sanitizeResolverTag normalises a resolver-supplied tag and rejects
// values that are empty, too long, or contain characters outside the
// BCP 47 subset (lowercase letters, digits, hyphen). Returns "" on
// rejection so the caller falls through to the next source.
func sanitizeResolverTag(tag string) string {
	tag = normalize(tag)
	if tag == "" || len(tag) > maxResolverTagLen {
		return ""
	}
	for _, ch := range tag {
		if !((ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-') {
			return ""
		}
	}
	return tag
}

// availableLocales returns the set of locales the catalog can serve,
// keyed by tag. Empty (but non-nil) when the translator has no catalog.
func availableLocales(tr *Translator) map[string]struct{} {
	available := map[string]struct{}{}
	if tr != nil && tr.catalog != nil {
		for _, l := range tr.catalog.Locales() {
			available[l] = struct{}{}
		}
	}
	return available
}

// matchCatalog returns the catalog locale matching tag (trying tag
// fallbacks: "fr-ca" → "fr"), or "" when nothing matches.
func matchCatalog(tag string, available map[string]struct{}) string {
	for _, candidate := range tagFallbacks(tag) {
		if _, ok := available[candidate]; ok {
			return candidate
		}
	}
	return ""
}

// Middleware returns an HTTP middleware that resolves the caller's
// locale and attaches it to the request context. Handlers downstream
// call [T] / [Translator.T] with `r.Context()`.
//
// Resolution order: locale resolver(s) (see [WithLocaleResolver]) →
// explicit `X-Locale` header → highest-quality `Accept-Language` entry
// that matches a catalog locale → fallback. Pass resolvers to add a
// stored per-user locale (e.g. a cookie) that wins over the headers.
//
// If an `X-Locale` header is set, it wins outright over Accept-Language
// — useful for tests and for apps that already do locale routing.
//
// Locale negotiation falls through to the Translator's fallback when no
// Accept-Language entry matches any catalog locale.
func Middleware(tr *Translator, opts ...NegotiateOption) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			loc := Negotiate(tr, r, opts...)
			ctx := WithContext(r.Context(), loc)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// Negotiate picks the best locale for the request. Resolution order:
// locale resolver(s) → explicit X-Locale header → highest-quality
// Accept-Language entry that matches a catalog locale → fallback.
//
// A resolver value only wins when it matches a catalog locale (after
// tag fallbacks); otherwise the resolver is ignored and negotiation
// continues with the headers, so a garbage cookie cannot force an
// unsupported locale. Exposed separately so callers that don't want
// the middleware can still drive locale resolution.
func Negotiate(tr *Translator, r *http.Request, opts ...NegotiateOption) Locale {
	cfg := negotiateConfig{}
	for _, o := range opts {
		o(&cfg)
	}
	available := availableLocales(tr)

	// 1. Custom resolver(s) — a stored per-user locale wins when it
	//    matches the catalog; a garbage/unknown value falls through.
	for _, res := range cfg.resolvers {
		if res == nil {
			continue
		}
		tag, ok := res(r)
		if !ok {
			continue
		}
		tag = sanitizeResolverTag(tag)
		if tag == "" {
			continue
		}
		if tr == nil {
			// No catalog to validate against — accept the resolver's
			// value, mirroring the raw Accept-Language behaviour below.
			return Locale{Tag: tag}
		}
		if hit := matchCatalog(tag, available); hit != "" {
			return Locale{Tag: hit}
		}
	}

	// 2. X-Locale header — wins outright (backward-compatible).
	if forced := normalize(r.Header.Get("X-Locale")); forced != "" {
		return Locale{Tag: forced}
	}

	// 3. Accept-Language.
	if tr == nil {
		return Locale{Tag: normalize(r.Header.Get("Accept-Language"))}
	}
	for _, want := range parseAcceptLanguage(r.Header.Get("Accept-Language")) {
		if hit := matchCatalog(want, available); hit != "" {
			return Locale{Tag: hit}
		}
	}

	// 4. Fallback.
	if tr.fallback != "" {
		return Locale{Tag: tr.fallback}
	}
	return Locale{}
}

// ----- helpers -------------------------------------------------------------

// parseAcceptLanguage returns tags ordered by q-value (highest first).
// Bad or zero-q entries are skipped.
func parseAcceptLanguage(header string) []string {
	if header == "" {
		return nil
	}
	type entry struct {
		tag string
		q   float64
	}
	// Bound the work an attacker-controlled header can force: RFC-realistic
	// clients send a handful of language ranges. Cap the number of accepted
	// entries so a header packed with commas (up to MaxHeaderBytes) cannot
	// amplify into hundreds of thousands of allocations + an O(n log n) sort.
	const maxEntries = 32
	var out []entry
	for _, part := range strings.Split(header, ",") {
		if len(out) >= maxEntries {
			break
		}
		seg := strings.TrimSpace(part)
		if seg == "" {
			continue
		}
		tag, q := seg, 1.0
		if i := strings.Index(seg, ";"); i >= 0 {
			tag = strings.TrimSpace(seg[:i])
			params := seg[i+1:]
			for _, kv := range strings.Split(params, ";") {
				kv = strings.TrimSpace(kv)
				if strings.HasPrefix(kv, "q=") {
					if f, err := strconv.ParseFloat(kv[2:], 64); err == nil {
						q = f
					}
				}
			}
		}
		if q <= 0 {
			continue
		}
		out = append(out, entry{tag: normalize(tag), q: q})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].q > out[j].q })
	tags := make([]string, 0, len(out))
	for _, e := range out {
		tags = append(tags, e.tag)
	}
	return tags
}

// tagFallbacks returns progressively shorter forms of a BCP 47 tag:
// "fr-ca" → ["fr-ca", "fr"]. Empty input yields nil.
func tagFallbacks(tag string) []string {
	tag = normalize(tag)
	if tag == "" {
		return nil
	}
	out := []string{tag}
	if i := strings.IndexByte(tag, '-'); i > 0 {
		out = append(out, tag[:i])
	}
	return out
}

// normalize lower-cases a tag and trims surrounding whitespace.
func normalize(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// mergeParams flattens an optional variadic into a single map; nil
// entries are ignored. Returns a fresh map so callers can't mutate
// the caller's input.
func mergeParams(in []map[string]any) map[string]any {
	out := map[string]any{}
	for _, p := range in {
		for k, v := range p {
			out[k] = v
		}
	}
	return out
}

// extractCount looks for a "count" or "n" key holding an integer-ish
// value. Returns (n, true) on success.
func extractCount(p map[string]any) (int, bool) {
	for _, k := range []string{"count", "n", "Count", "N"} {
		if v, ok := p[k]; ok {
			switch x := v.(type) {
			case int:
				return x, true
			case int32:
				return int(x), true
			case int64:
				return int(x), true
			case uint:
				return int(x), true
			case uint32:
				return int(x), true
			case uint64:
				return int(x), true
			case float32:
				return int(x), true
			case float64:
				return int(x), true
			case string:
				if n, err := strconv.Atoi(x); err == nil {
					return n, true
				}
			}
		}
	}
	return 0, false
}

// interpolate replaces {{name}} tokens with stringified params. Names
// are looked up case-sensitively. Unknown placeholders are left as-is
// — easier to spot during development than silently empty.
func interpolate(s string, params map[string]any) string {
	if s == "" || len(params) == 0 || !strings.Contains(s, "{{") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		j := strings.Index(s[i:], "{{")
		if j < 0 {
			b.WriteString(s[i:])
			break
		}
		b.WriteString(s[i : i+j])
		k := strings.Index(s[i+j+2:], "}}")
		if k < 0 {
			b.WriteString(s[i+j:])
			break
		}
		name := strings.TrimSpace(s[i+j+2 : i+j+2+k])
		if v, ok := params[name]; ok {
			fmt.Fprint(&b, v)
		} else {
			// Leave the placeholder intact so it's visible.
			b.WriteString(s[i+j : i+j+2+k+2])
		}
		i = i + j + 2 + k + 2
	}
	return b.String()
}
