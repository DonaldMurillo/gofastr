// Package i18n is a small internationalization primitive for GoFastr
// apps: locale negotiation from `Accept-Language`, JSON-backed message
// catalogs with `{{placeholder}}` interpolation, and CLDR plural
// categories (zero / one / two / few / many / other) for every
// language, through golang.org/x/text.
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
	"maps"
	"net/http"
	"path"
	"slices"
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

// String renders the tag, useful in templates that already know they
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
// fall back to the bare key, useful while bootstrapping a new app.
type Catalog interface {
	Get(locale, key string) (Message, bool)
	Locales() []string
}

// Translator looks up keys in a Catalog, picks a plural message's form
// by the locale's CLDR rules, and interpolates {{placeholders}} from
// params.
type Translator struct {
	catalog  Catalog
	fallback string
}

// NewTranslator wraps a Catalog with a fallback locale (used when the
// request locale is missing entries). Pass an empty fallback to
// disable fallback (missing keys return the bare key).
func NewTranslator(c Catalog, fallback string) *Translator {
	return &Translator{
		catalog:  c,
		fallback: normalize(fallback),
	}
}

// Fallback returns the locale the translator answers in when the
// reader's locale lacks a key, "" when it has none.
func (t *Translator) Fallback() string {
	if t == nil {
		return ""
	}
	return t.fallback
}

// Locales returns the catalog's locales, sorted, or nil without a
// catalog.
func (t *Translator) Locales() []string {
	if t == nil || t.catalog == nil {
		return nil
	}
	return slices.Sorted(slices.Values(t.catalog.Locales()))
}

// T returns the translated message for key, interpolating any
// {{placeholder}} tokens from the first params map. When the message
// is a plural and params contains a numeric "count" / "n", the
// locale's CLDR rule picks the form.
//
// Lookup order: ctx locale → fallback locale → bare key. Empty key
// returns empty string.
func (t *Translator) T(ctx context.Context, key string, params ...map[string]any) string {
	if t == nil || key == "" {
		return key
	}
	p := mergeParams(params)
	tag := FromContext(ctx).Tag
	if s, ok := t.translate(tag, key, p); ok {
		return s
	}
	if t.fallback != "" {
		if s, ok := t.translate(t.fallback, key, p); ok {
			return s
		}
	}
	return key
}

// translate renders key for tag, walking the catalog with progressive
// language tag truncation (so "fr-CA" falls back to "fr" before missing
// entirely). An entry with no form for the request (see render) is a
// miss like an absent one, so the walk goes on to the next candidate.
func (t *Translator) translate(tag, key string, params map[string]any) (string, bool) {
	if t.catalog == nil {
		return "", false
	}
	for _, candidate := range tagFallbacks(tag) {
		if m, ok := t.catalog.Get(candidate, key); ok {
			if s, ok := t.render(tag, m, params); ok {
				return s, true
			}
		}
	}
	return "", false
}

// render applies plural selection and interpolates placeholders, a
// number param written the way tag's locale writes numbers. The plural
// form is picked from the raw count, before it is formatted. ok is false
// for a plural message holding neither the selected form nor "other",
// with no Text to fall back on: it has nothing to say for this count,
// and rendering "" would pass for a translation.
func (t *Translator) render(tag string, m Message, params map[string]any) (string, bool) {
	loc := resolveCLDR(tag).loc
	if len(m.Plural) > 0 {
		cat := "other" // no count supplied: the "other" variant
		if ops, ok := countOperands(params); ok {
			cat = pluralCategory(tag, ops)
		}
		if v, has := m.Plural[cat]; has {
			return fill(v, params, loc), true
		}
		if v, has := m.Plural["other"]; has {
			return fill(v, params, loc), true
		}
		if m.Text == "" {
			return "", false
		}
	}
	return fill(m.Text, params, loc), true
}

// ----- catalog implementations ---------------------------------------------

// MapCatalog is the simplest in-memory Catalog, useful in tests and
// for embedding small string sets directly in code.
type MapCatalog struct {
	// mu guards Entries. Set is documented as a test convenience, but
	// MapCatalog is also the type LoadJSONCatalog returns, so a host can
	// hold a live one and call Set while request goroutines are in Get.
	mu      sync.RWMutex
	Entries map[string]map[string]Message // locale → key → message
}

// NewMapCatalog returns an empty MapCatalog.
func NewMapCatalog() *MapCatalog {
	return &MapCatalog{Entries: map[string]map[string]Message{}}
}

// Set writes a message into the catalog. Convenience for tests.
func (c *MapCatalog) Set(locale, key string, m Message) {
	tag := normalize(locale)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.Entries == nil {
		c.Entries = map[string]map[string]Message{}
	}
	if c.Entries[tag] == nil {
		c.Entries[tag] = map[string]Message{}
	}
	m.Plural = maps.Clone(m.Plural)
	c.Entries[tag][key] = m
}

// Get implements Catalog.
func (c *MapCatalog) Get(locale, key string) (Message, bool) {
	if c == nil {
		return Message{}, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if m, ok := c.Entries[normalize(locale)][key]; ok {
		m.Plural = maps.Clone(m.Plural)
		return m, true
	}
	return Message{}, false
}

// Locales implements Catalog.
func (c *MapCatalog) Locales() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return slices.Sorted(maps.Keys(c.Entries))
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
		// io/fs paths are always slash-separated; filepath.Join would
		// emit backslashes on Windows and fail every lookup.
		raw, err := fs.ReadFile(fsys, path.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("i18n: read %s: %w", e.Name(), err)
		}
		var doc map[string]any
		//gofastr:allow(floatnum) flatten keeps only strings and nested maps; a number in a catalog is never read
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
	resolvers []LocaleResolver
}

// LocaleResolver reads a stored per-user locale preference off a
// request (see [WithLocaleResolver]).
type LocaleResolver struct {
	// Resolve returns a BCP 47 tag and ok=true when the request names a
	// preference; ok=false falls through to header-based negotiation.
	Resolve func(*http.Request) (string, bool)
	// Vary names every request header Resolve reads ("Cookie" for a
	// cookie). [Middleware] adds each to the response's Vary, beside
	// Accept-Language and X-Locale, so a shared cache (a CDN, the cache
	// battery) keys its copies on them instead of replaying one
	// reader's language to the next.
	Vary []string
}

// WithLocaleResolver installs a custom locale resolver consulted BEFORE
// the X-Locale / Accept-Language headers, for a preference such as a
// stored per-user locale cookie. A resolver with no Resolve func is
// ignored.
//
// The returned tag is matched against the catalog's locales (with the
// same tag fallbacks as Accept-Language: "fr-ca" → "fr"). An unknown,
// malformed, or over-long value falls through to the next source
// rather than producing an unmatched locale. Resolver values are
// attacker-controlled (cookies), so they are length- and
// character-bounded before any matching.
func WithLocaleResolver(r LocaleResolver) NegotiateOption {
	return func(c *negotiateConfig) {
		if r.Resolve != nil {
			c.resolvers = append(c.resolvers, r)
		}
	}
}

// CookieLocale returns a resolver that reads the caller's preferred
// locale from the named cookie and names Cookie in its Vary. A missing
// or empty cookie returns ok=false so negotiation falls through to the
// headers. Pair with [WithLocaleResolver] and a set-cookie handler for
// per-user locale switching.
func CookieLocale(name string) LocaleResolver {
	return LocaleResolver{
		Resolve: func(r *http.Request) (string, bool) {
			if r == nil || name == "" {
				return "", false
			}
			c, err := r.Cookie(name)
			if err != nil {
				return "", false
			}
			return c.Value, c.Value != ""
		},
		Vary: []string{"Cookie"},
	}
}

// maxResolverTagLen bounds how long an attacker-controlled tag may be
// before it is rejected. Mirrors the Accept-Language bounding: real
// BCP 47 tags are well under this.
const maxResolverTagLen = 35

// sanitizeTag normalises an attacker-controlled locale tag and rejects
// values that are empty, too long, or contain characters outside the
// BCP 47 subset (lowercase letters, digits, hyphen). Returns "" on
// rejection so the caller falls through to the next source.
//
// EVERY externally-supplied tag goes through this: resolver values
// (cookies), the X-Locale header, and Accept-Language entries, because
// each of them can become the Locale.Tag that a host's Catalog.Get
// receives as a lookup key, and Catalog is a documented third-party
// extension point. Bounding only the cookie left the two header
// sources, which are equally attacker-controlled, unguarded.
func sanitizeTag(tag string) string {
	tag = normalize(tag)
	if tag == "" || len(tag) > maxResolverTagLen {
		return ""
	}
	if strings.HasPrefix(tag, "-") || strings.HasSuffix(tag, "-") || strings.Contains(tag, "--") {
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

// matchAccepted is matchCatalog for an Accept-Language entry: a
// fallback the header refused with q=0 is skipped, so "en-GB" does not
// truncate to a refused "en".
func matchAccepted(tag string, available map[string]struct{}, refused []string) string {
	for _, candidate := range tagFallbacks(tag) {
		if _, ok := available[candidate]; ok && !slices.Contains(refused, candidate) {
			return candidate
		}
	}
	return ""
}

// Middleware returns an HTTP middleware that resolves the caller's
// locale and attaches it, with tr, to the request context. Handlers
// downstream call [T], [Translator.T] or [Phrase.Text] with
// `r.Context()`.
//
// Resolution order: locale resolver(s) (see [WithLocaleResolver]) →
// explicit `X-Locale` header → highest-quality `Accept-Language` entry
// that matches a catalog locale → fallback. Pass resolvers to add a
// stored per-user locale (e.g. a cookie) that wins over the headers.
//
// If an `X-Locale` header is set, it wins outright over Accept-Language,
// useful for tests and for apps that already do locale routing.
//
// Locale negotiation falls through to the Translator's fallback when no
// Accept-Language entry matches any catalog locale.
func Middleware(tr *Translator, opts ...NegotiateOption) func(http.Handler) http.Handler {
	cfg := negotiateConfig{}
	for _, o := range opts {
		o(&cfg)
	}
	vary := []string{"Accept-Language", "X-Locale"}
	for _, res := range cfg.resolvers {
		for _, h := range res.Vary {
			if h = http.CanonicalHeaderKey(strings.TrimSpace(h)); h != "" && !slices.Contains(vary, h) {
				vary = append(vary, h)
			}
		}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			loc := Negotiate(tr, r, opts...)
			// A locale read from the request makes every response
			// depend on what it was read from: the locale headers and
			// each header a resolver names (a cookie). Without the Vary
			// a shared cache (a CDN, or the cache battery) stores the
			// first reader's language and serves it to everyone. A
			// locale from the URL needs none: the URL already differs.
			if _, _, fromPath := PathLocale(r.Context()); !fromPath {
				for _, h := range vary {
					w.Header().Add("Vary", h)
				}
			}
			ctx := WithTranslator(WithContext(r.Context(), loc), tr)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// Negotiate picks the best locale for the request. Resolution order:
// the URL's locale ([WithPathLocale]) → locale resolver(s) → explicit
// X-Locale header → highest-quality Accept-Language entry that matches
// a catalog locale → fallback.
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
	// 0. The URL's locale: an app that routes by locale prefix lets the
	//    path alone decide, so a page's hreflang is the language it
	//    serves whatever the cookie or the headers say.
	if _, tag, ok := PathLocale(r.Context()); ok {
		return Locale{Tag: tag}
	}
	available := availableLocales(tr)

	// 1. Custom resolver(s): a stored per-user locale wins when it
	//    matches the catalog; a garbage/unknown value falls through.
	for _, res := range cfg.resolvers {
		tag, ok := res.Resolve(r)
		if !ok {
			continue
		}
		tag = sanitizeTag(tag)
		if tag == "" {
			continue
		}
		if tr == nil {
			// No catalog to validate against. Accept the resolver's
			// value, mirroring the raw Accept-Language behaviour below.
			return Locale{Tag: tag}
		}
		if hit := matchCatalog(tag, available); hit != "" {
			return Locale{Tag: hit}
		}
	}

	// 2. X-Locale header: wins outright (backward-compatible), but is
	//    bounded exactly like a resolver value first. X-Locale is just as
	//    attacker-controlled as a cookie and it short-circuits every
	//    later check, so an unbounded value here becomes the Locale.Tag
	//    that flows to tagFallbacks → Catalog.Get. Catalog is a
	//    third-party extension point (the documented wiring is
	//    LoadJSONCatalog over an fs.FS), so a host with a lazily-loading
	//    FS/DB catalog would receive "../../etc/passwd" as a lookup key.
	//    Rejected values fall through to Accept-Language rather than
	//    becoming the tag.
	if forced := sanitizeTag(r.Header.Get("X-Locale")); forced != "" {
		return Locale{Tag: forced}
	}

	// 3. Accept-Language.
	if tr == nil {
		// No catalog to match against. Take the highest-quality entry
		// that survives bounding, NOT the whole raw header, which used
		// to be handed back as a "tag" verbatim.
		tags, _ := parseAcceptLanguage(r.Header.Get("Accept-Language"))
		for _, want := range tags {
			if want != "*" {
				return Locale{Tag: want}
			}
		}
		return Locale{}
	}
	tags, refused := parseAcceptLanguage(r.Header.Get("Accept-Language"))
	for _, want := range tags {
		if want == "*" {
			if hit := wildcardMatch(tr.fallback, available, refused); hit != "" {
				return Locale{Tag: hit}
			}
			continue
		}
		if hit := matchAccepted(want, available, refused); hit != "" {
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

// wildcardMatch answers an Accept-Language "*": any catalog locale the
// header did not refuse with q=0, the fallback first, else the first in
// sorted order. "" when every locale was refused.
func wildcardMatch(fallback string, available map[string]struct{}, refused []string) string {
	ok := func(l string) bool {
		_, has := available[l]
		return has && !slices.Contains(refused, l)
	}
	if ok(fallback) {
		return fallback
	}
	for _, l := range slices.Sorted(maps.Keys(available)) {
		if ok(l) {
			return l
		}
	}
	return ""
}

// parseAcceptLanguage returns tags ordered by q-value (highest first),
// and the tags refused with q=0. Every tag except "*" has passed
// [sanitizeTag]; an entry it rejects is dropped. An entry whose q is not a number in
// [0, 1] is skipped: kept at the default q=1 it outranked every
// well-formed entry.
func parseAcceptLanguage(header string) (tags, refused []string) {
	if header == "" {
		return nil, nil
	}
	type entry struct {
		tag string
		q   float64
	}
	// Bound the work an attacker-controlled header can force: RFC-realistic
	// clients send a handful of language ranges.
	//
	// Capping len(out) alone was not enough. strings.Split materialises
	// the ENTIRE comma-separated slice before the cap is ever consulted,
	// so a ~600KB header still allocated ~3.2MB and ~500k strings per
	// request, 12000x a normal header, and the inner ";" split was
	// uncapped on top of that. Scan the header in place instead, and
	// bound the number of segments EXAMINED, not only accepted.
	const (
		maxEntries  = 32
		maxSegments = 512
		maxParams   = 8
	)
	var out []entry
	rest := header
	for segments := 0; rest != "" && len(out)+len(refused) < maxEntries && segments < maxSegments; segments++ {
		part := rest
		if i := strings.IndexByte(rest, ','); i >= 0 {
			part, rest = rest[:i], rest[i+1:]
		} else {
			rest = ""
		}
		seg := strings.TrimSpace(part)
		if seg == "" {
			continue
		}
		tag, q, valid := seg, 1.0, true
		if before, after, ok := strings.Cut(seg, ";"); ok {
			tag = strings.TrimSpace(before)
			params := after
			for n := 0; params != "" && n < maxParams; n++ {
				kv := params
				if j := strings.IndexByte(params, ';'); j >= 0 {
					kv, params = params[:j], params[j+1:]
				} else {
					params = ""
				}
				kv = strings.TrimSpace(kv)
				if strings.HasPrefix(kv, "q=") {
					f, err := strconv.ParseFloat(kv[2:], 64)
					// NaN fails both bounds, so this refuses it too.
					valid = err == nil && f >= 0 && f <= 1
					q = f
				}
			}
		}
		if !valid {
			continue
		}
		// Bound every range here, once, so no caller can match on a raw
		// one: an ~800 KB "en-x-x-…" range made tagFallbacks build
		// ~400k strings per request on the catalog path, and each map
		// probe hashed its whole prefix. "*" is the one non-tag range.
		if tag = normalize(tag); tag != "*" {
			if tag = sanitizeTag(tag); tag == "" {
				continue
			}
		}
		if q == 0 {
			refused = append(refused, tag)
			continue
		}
		out = append(out, entry{tag: tag, q: q})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].q > out[j].q })
	tags = make([]string, 0, len(out))
	for _, e := range out {
		tags = append(tags, e.tag)
	}
	return tags, refused
}

// tagFallbacks returns progressively shorter forms of a BCP 47 tag, one
// subtag off the right at a time, with the language-script form CLDR
// reads the tag as placed before the bare language: "zh-cn" →
// ["zh-cn", "zh-hans", "zh"]. Dropping subtags alone sent zh-TW to the
// Simplified "zh" catalog. A script that is not the language's usual
// one ends the chain, the way CLDR makes root the parent of zh-Hant and
// sr-Latn: "zh-tw" → ["zh-tw", "zh-hant"] and "zh-hant-hk" →
// ["zh-hant-hk", "zh-hant"], never the Simplified "zh". Empty input
// yields nil.
func tagFallbacks(tag string) []string {
	tag = normalize(tag)
	if tag == "" {
		return nil
	}
	out := []string{tag}
	for t := tag; ; {
		i := strings.LastIndexByte(t, '-')
		if i <= 0 {
			break
		}
		t = t[:i]
		out = append(out, t)
	}
	form, keepBase := scriptFallback(tag, out[len(out)-1])
	if form == "" {
		return out
	}
	if len(out) == 1 {
		// The bare language itself: its own catalog first, then the
		// script form ("zh" → zh, zh-hans).
		return []string{tag, form}
	}
	base := out[len(out)-1]
	out = out[:len(out)-1]
	if !slices.Contains(out, form) {
		out = append(out, form)
	}
	if keepBase {
		out = append(out, base)
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
		maps.Copy(out, p)
	}
	return out
}

// Fill replaces s's {{name}} placeholders with stringified params.
// Names are looked up case-sensitively. A value is written as given and
// never rescanned. An unknown placeholder is left as written, easier to
// spot than an empty gap. Numbers print as Go prints them; text a
// reader sees goes through [FillContext], which writes them the
// reader's way.
func Fill(s string, params map[string]any) string {
	return fill(s, params, nil)
}

// FillContext is the substitution [Phrase.Text] applies, for a template
// that arrives as a plain string (a headless.Strings field, a catalog
// value read by hand): [Fill], with every number param (any integer or
// float kind that is not a fmt.Stringer or error) written in ctx's
// locale, so 1234 reads "1 234" in French. Pass the number, not
// strconv.Itoa of it.
func FillContext(ctx context.Context, s string, params map[string]any) string {
	return fill(s, params, cldrFor(ctx).loc)
}

// fill substitutes params into s; a non-nil loc writes number params
// in that locale.
func fill(s string, params map[string]any, loc *cldrLocale) string {
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
			if n, isNum := loc.formatParam(v); isNum {
				b.WriteString(n)
			} else {
				fmt.Fprint(&b, v)
			}
		} else {
			// Leave the placeholder intact so it's visible.
			b.WriteString(s[i+j : i+j+2+k+2])
		}
		i = i + j + 2 + k + 2
	}
	return b.String()
}
