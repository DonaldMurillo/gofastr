package i18n

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// PathLocales is the locale set of an app that names the reader's
// language in the URL: the default locale's pages sit at their plain
// paths, and every other locale's pages sit under /<tag>/ ("/es/pricing").
// framework.WithLocalePrefixes builds one from the app's translator.
type PathLocales struct {
	// Default is the locale whose pages carry no prefix: the
	// translator's fallback.
	Default string
	// Prefixed lists the other catalog locales, sorted. Each is a
	// lowercase ASCII tag, matched against the first path segment
	// exactly: "/ES/" is a plain route.
	Prefixed []string
}

// NewPathLocales reads tr's locale set: its fallback is the default and
// its other catalog locales are prefixed. It refuses a translator with
// no fallback, and a catalog locale that is not a lowercase tag of
// letters, digits and single hyphens, which cannot be a path segment.
func NewPathLocales(tr *Translator) (PathLocales, error) {
	if tr == nil {
		return PathLocales{}, errors.New("i18n: path locales need a translator")
	}
	def := tr.Fallback()
	if def == "" {
		return PathLocales{}, errors.New("i18n: path locales need a fallback locale: it is the one with no prefix")
	}
	set := PathLocales{Default: def}
	for _, tag := range tr.Locales() {
		if sanitizeTag(tag) != tag {
			return PathLocales{}, fmt.Errorf("i18n: catalog locale %q cannot be a path prefix: use a lowercase tag such as \"fr-ca\"", tag)
		}
		if tag != def {
			set.Prefixed = append(set.Prefixed, tag)
		}
	}
	return set, nil
}

// Split reads a locale prefix off path. tag is the locale its first
// segment names, the default included, or "" when it names none; rest
// is the path without that segment ("/" for "/es"), or path itself when
// tag is "". A caller redirects a default-locale prefix to rest, since
// the default locale's pages have no prefix.
func (p PathLocales) Split(path string) (tag, rest string) {
	if len(path) < 2 || path[0] != '/' {
		return "", path
	}
	seg, after, _ := strings.Cut(path[1:], "/")
	if seg != p.Default && !slices.Contains(p.Prefixed, seg) {
		return "", path
	}
	return seg, "/" + after
}

// prefix returns the path prefix of tag in this set: "" for the default
// locale or a tag outside the set, "/<tag>" otherwise.
func (p PathLocales) prefix(tag string) string {
	if tag == p.Default || !slices.Contains(p.Prefixed, tag) {
		return ""
	}
	return "/" + tag
}

type pathLocaleKey struct{}

type pathLocale struct {
	set PathLocales
	tag string
}

// WithPathLocale records that the request's URL named locale tag of
// set; tag is set.Default for a path with no prefix. [Negotiate] then
// answers tag over every other source, so the URL alone decides the
// language. The framework's router does this for an app built with
// framework.WithLocalePrefixes.
func WithPathLocale(ctx context.Context, set PathLocales, tag string) context.Context {
	return context.WithValue(ctx, pathLocaleKey{}, pathLocale{set: set, tag: tag})
}

// PathLocale returns the locale set and the tag the request's URL
// named, and false when the app does not route by locale prefix.
func PathLocale(ctx context.Context) (set PathLocales, tag string, ok bool) {
	if ctx == nil {
		return PathLocales{}, "", false
	}
	v, ok := ctx.Value(pathLocaleKey{}).(pathLocale)
	return v.set, v.tag, ok
}

// PathPrefix returns the request's locale prefix ("/es"), or "" for the
// default locale and for an app that does not route by locale prefix.
// Code that builds a URL the response rewriter cannot see (a
// meta-refresh, a JSON field, an SSE push) puts it in front of a
// root-relative path.
func PathPrefix(ctx context.Context) string {
	set, tag, ok := PathLocale(ctx)
	if !ok {
		return ""
	}
	return set.prefix(tag)
}

// Path returns the root-relative path as the page in locale tag:
// "/es/pricing" for ("es", "/pricing"), the plain path for the default
// locale or a tag outside the set.
func (p PathLocales) Path(tag, path string) string {
	pre := p.prefix(tag)
	if pre == "" {
		return path
	}
	if path == "/" {
		return pre
	}
	// "/?q=1": the root with a query.
	if len(path) > 1 && path[0] == '/' && path[1] == '?' {
		return pre + path[1:]
	}
	return pre + path
}

// All returns the default locale followed by the prefixed ones.
func (p PathLocales) All() []string {
	return append([]string{p.Default}, p.Prefixed...)
}

// LocalePath returns path as the page in locale tag: "/es/pricing" for
// ("es", "/pricing"), the plain path for the default locale. A language
// switcher links each entry with it and marks the anchor with hreflang,
// which the response rewriter leaves alone. Without path locale routing,
// or for a tag outside the set, it returns path unchanged.
func LocalePath(ctx context.Context, tag, path string) string {
	set, _, ok := PathLocale(ctx)
	if !ok {
		return path
	}
	return set.Path(tag, path)
}
