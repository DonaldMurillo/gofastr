package i18n

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// PathLocales is the locale set of an app that names the language in the URL:
// the default locale at plain paths, each other under /<tag>/.
type PathLocales struct {
	// Default is the unprefixed locale, the translator's fallback.
	Default string
	// Prefixed lists the other catalog locales, sorted.
	Prefixed []string
}

// NewPathLocales reads tr's locale set; it errors without a fallback or on a
// locale that is not a lowercase tag.
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

// Split reads a locale prefix off path, returning the tag ("" for none) and
// the rest of the path.
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

// WithPathLocale records that the URL named tag of set, which [Negotiate] then
// answers over every other source.
func WithPathLocale(ctx context.Context, set PathLocales, tag string) context.Context {
	return context.WithValue(ctx, pathLocaleKey{}, pathLocale{set: set, tag: tag})
}

// PathLocale returns the set and tag the URL named, false without one.
func PathLocale(ctx context.Context) (set PathLocales, tag string, ok bool) {
	if ctx == nil {
		return PathLocales{}, "", false
	}
	v, ok := ctx.Value(pathLocaleKey{}).(pathLocale)
	return v.set, v.tag, ok
}

// PathPrefix returns the request's locale prefix ("/es"), or "".
func PathPrefix(ctx context.Context) string {
	set, tag, ok := PathLocale(ctx)
	if !ok {
		return ""
	}
	return set.prefix(tag)
}

// Path returns path as the page in locale tag.
func (p PathLocales) Path(tag, path string) string {
	pre := p.prefix(tag)
	if pre == "" {
		return path
	}
	if path == "/" {
		return pre
	}
	if len(path) > 1 && path[0] == '/' && path[1] == '?' {
		return pre + path[1:]
	}
	return pre + path
}

// All returns the default locale followed by the prefixed ones.
func (p PathLocales) All() []string {
	return append([]string{p.Default}, p.Prefixed...)
}

// LocalePath returns path as the page in locale tag, unchanged without path
// locale routing or for a tag outside the set.
func LocalePath(ctx context.Context, tag, path string) string {
	set, _, ok := PathLocale(ctx)
	if !ok {
		return path
	}
	return set.Path(tag, path)
}
