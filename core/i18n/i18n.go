// Package i18n negotiates a request's locale, looks messages up in catalogs,
// fills {{placeholders}} and picks CLDR plural forms, and formats numbers,
// dates and lists the locale's way.
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

	"golang.org/x/text/language"

	"github.com/DonaldMurillo/gofastr/internal/asciicase"
)

// Locale is a lower-cased BCP 47 tag; an empty Tag states no preference.
type Locale struct {
	Tag string
}

// String returns the tag.
func (l Locale) String() string { return l.Tag }

// Message is one catalog entry: Text, or Plural forms by CLDR category.
type Message struct {
	Text   string
	Plural map[string]string
}

// Catalog looks messages up by locale and key.
type Catalog interface {
	Get(locale, key string) (Message, bool)
	Locales() []string
}

// Translator renders catalog messages for a locale.
type Translator struct {
	catalog  Catalog
	fallback string
}

// NewTranslator wraps c with a fallback locale; "" means none.
func NewTranslator(c Catalog, fallback string) *Translator {
	return &Translator{
		catalog:  c,
		fallback: normalize(fallback),
	}
}

// Fallback returns the fallback locale, "" when it has none.
func (t *Translator) Fallback() string {
	if t == nil {
		return ""
	}
	return t.fallback
}

// Locales returns the catalog's locales, sorted, or nil without a catalog.
func (t *Translator) Locales() []string {
	if t == nil || t.catalog == nil {
		return nil
	}
	return slices.Sorted(slices.Values(t.catalog.Locales()))
}

// T renders key for ctx's locale, then the fallback, else returns key,
// filling placeholders from params and picking a plural form by count or n.
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

func (t *Translator) render(tag string, m Message, params map[string]any) (string, bool) {
	loc := resolveCLDR(tag).loc
	if len(m.Plural) > 0 {
		cat := "other"
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

// MapCatalog is an in-memory Catalog, safe for concurrent use.
type MapCatalog struct {
	mu      sync.RWMutex
	Entries map[string]map[string]Message
}

// NewMapCatalog returns an empty MapCatalog.
func NewMapCatalog() *MapCatalog {
	return &MapCatalog{Entries: map[string]map[string]Message{}}
}

// Set stores a copy of m under locale and key.
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

// LoadJSONCatalog reads every <locale>.json in dir, flattening nested keys
// with dots and reading an object of plural categories as a plural message.
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

type ctxKey struct{}

// WithContext attaches l to ctx.
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

var (
	defaultMu sync.RWMutex
	def       *Translator
)

// SetDefault installs the Translator T uses; nil disables it.
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

// T is Default().T, or key without a default.
func T(ctx context.Context, key string, params ...map[string]any) string {
	d := Default()
	if d == nil {
		return key
	}
	return d.T(ctx, key, params...)
}

// NegotiateOption configures [Negotiate] and [Middleware].
type NegotiateOption func(*negotiateConfig)

type negotiateConfig struct {
	resolvers []LocaleResolver
}

// LocaleResolver reads a stored locale preference off a request.
type LocaleResolver struct {
	// Resolve returns a tag, ok=false to fall through to the headers.
	Resolve func(*http.Request) (string, bool)
	// Vary names the request headers Resolve reads.
	Vary []string
}

// WithLocaleResolver adds a resolver consulted before the headers; its value
// counts only when it is a well-formed tag the catalog has.
func WithLocaleResolver(r LocaleResolver) NegotiateOption {
	return func(c *negotiateConfig) {
		if r.Resolve != nil {
			c.resolvers = append(c.resolvers, r)
		}
	}
}

// CookieLocale returns a resolver reading the named cookie, varying on Cookie.
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

const maxResolverTagLen = 35

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

func availableLocales(tr *Translator) map[string]struct{} {
	available := map[string]struct{}{}
	if tr != nil && tr.catalog != nil {
		for _, l := range tr.catalog.Locales() {
			available[l] = struct{}{}
		}
	}
	return available
}

func matchCatalog(tag string, available map[string]struct{}) string {
	for _, candidate := range tagFallbacks(tag) {
		if _, ok := available[candidate]; ok {
			return candidate
		}
	}
	return ""
}

func canonicalAlias(tag, base string) string {
	if len(tag) > maxCLDRTag {
		return ""
	}
	canonical, err := language.Parse(tag)
	if err != nil {
		return ""
	}
	b, _, _ := canonical.Raw()
	alias := asciicase.Lower(canonical.String())
	if b.String() == base || alias == tag {
		return ""
	}
	return alias
}

func baseOf(tag string) string {
	if i := strings.IndexByte(tag, '-'); i > 0 {
		return tag[:i]
	}
	return tag
}

func matchAccepted(tag string, available map[string]struct{}, refused []string) string {
	for _, candidate := range tagFallbacks(tag) {
		if _, ok := available[candidate]; ok && !slices.Contains(refused, candidate) {
			return candidate
		}
	}
	return ""
}

// Middleware negotiates each request's locale and attaches it and tr to the
// context, adding Vary for the headers the locale was read from.
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

// Negotiate picks the request's locale: the URL's, then a resolver's, then
// X-Locale, then the best Accept-Language match, then the fallback.
func Negotiate(tr *Translator, r *http.Request, opts ...NegotiateOption) Locale {
	cfg := negotiateConfig{}
	for _, o := range opts {
		o(&cfg)
	}
	if _, tag, ok := PathLocale(r.Context()); ok {
		return Locale{Tag: tag}
	}
	available := availableLocales(tr)

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
			return Locale{Tag: tag}
		}
		if hit := matchCatalog(tag, available); hit != "" {
			return Locale{Tag: hit}
		}
	}

	if forced := sanitizeTag(r.Header.Get("X-Locale")); forced != "" {
		return Locale{Tag: forced}
	}

	if tr == nil {
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

	if tr.fallback != "" {
		return Locale{Tag: tr.fallback}
	}
	return Locale{}
}

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

func parseAcceptLanguage(header string) (tags, refused []string) {
	if header == "" {
		return nil, nil
	}
	type entry struct {
		tag string
		q   float64
	}
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
					valid = err == nil && f >= 0 && f <= 1
					q = f
				}
			}
		}
		if !valid {
			continue
		}
		if tag = normalize(tag); tag != "*" {
			if tag = sanitizeTag(tag); tag == "" {
				continue
			}
		}
		if q == 0 {
			refused = append(refused, tag)
			if alias := canonicalAlias(tag, baseOf(tag)); alias != "" {
				refused = append(refused, alias)
			}
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
	if alias := canonicalAlias(tag, out[len(out)-1]); alias != "" {
		return append(out, tagFallbacks(alias)...)
	}
	form, keepBase := scriptFallback(tag, out[len(out)-1])
	if form == "" {
		return out
	}
	if len(out) == 1 {
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

func normalize(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func mergeParams(in []map[string]any) map[string]any {
	out := map[string]any{}
	for _, p := range in {
		maps.Copy(out, p)
	}
	return out
}

// Fill replaces s's {{name}} placeholders with params, written as given and
// never rescanned, leaving an unknown one as written.
func Fill(s string, params map[string]any) string {
	return fill(s, params, nil)
}

// FillContext is [Fill] with number params written in ctx's locale.
func FillContext(ctx context.Context, s string, params map[string]any) string {
	return fill(s, params, cldrFor(ctx).loc)
}

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
			b.WriteString(s[i+j : i+j+2+k+2])
		}
		i = i + j + 2 + k + 2
	}
	return b.String()
}
