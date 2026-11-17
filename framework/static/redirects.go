package static

import (
	"context"
	"errors"
	"fmt"
	stdhtml "html"
	"io/fs"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// writeRedirects gives every exact redirect route (app.Redirect) a page
// at its path, so a link or bookmark to the old URL still lands: a meta
// refresh to the chain's final target, with a plain link as the
// fallback, no script and no style, under the export's own CSP. It also
// writes a `_redirects` rule per redirect (`/old /new 308`, the live
// status) for Netlify and Cloudflare Pages, the hosts that read the
// `_headers` file beside it, which then answer with a real redirect. A
// `_redirects` in the static source is appended after these rules.
//
// A pattern redirect (app.RedirectPattern) matches URLs no file can
// stand for; it is logged as skipped and its URLs answer 404.html. A
// redirect chain that never ends (the live router fails it closed) is
// skipped too. With path locales each locale tree gets its own page, its
// target under the same prefix.
func (b *Builder) writeRedirects(ctx context.Context, pages map[string]bool, rendered *fileSet, owner map[string]string) error {
	locales, byPrefix := b.Host.PathLocales()
	tags := []string{""}
	if byPrefix {
		tags = locales.All()
	}
	var rules strings.Builder
	for _, route := range b.Host.App.Routes() {
		if route.RedirectTo == "" {
			continue
		}
		if b.routeExcluded(route.Path) {
			b.log("skipped excluded redirect %s", route.Path)
			continue
		}
		if strings.Contains(route.Path, ":") {
			slog.Warn("static: skipped pattern redirect: a static host cannot match a pattern, so its URLs answer the export's 404 page; the live server keeps redirecting them",
				"from", route.Path, "to", route.RedirectTo)
			continue
		}
		target, ok := b.Host.App.ResolveRedirect(route.Path)
		if !ok {
			slog.Warn("static: skipped redirect whose chain does not end", "from", route.Path)
			continue
		}
		for _, tag := range tags {
			from, to, lctx := route.Path, target, ctx
			if byPrefix {
				from, to = locales.Path(tag, from), b.localeTarget(locales, tag, to)
				lctx = localeContext(ctx, locales, tag)
			}
			href := b.redirectHref(to, pages)
			rel := filepath.ToSlash(pathToFile(from))
			// A redirect file that differs from a page's, or another
			// redirect's, only in case is the same file on a
			// case-insensitive filesystem; it would replace that page.
			if prev, dup := owner[foldPath(rel)]; dup {
				return fmt.Errorf("static: pages %q and %q both export to %s; give one of them another path", prev, from, rel)
			}
			owner[foldPath(rel)] = from
			dst := filepath.Join(b.OutDir, filepath.FromSlash(rel))
			if err := b.ensureContained(dst); err != nil {
				return err
			}
			if err := writeUnder(b.OutDir, dst, []byte(redirectPage(lctx, href, b.policy()))); err != nil {
				return err
			}
			rendered.add(rel)
			fmt.Fprintf(&rules, "%s %s 308\n", urlBytesEscaper(b.BasePath+from), href)
			b.log("redirect %s -> %s", from, href)
		}
	}
	dst := filepath.Join(b.OutDir, "_redirects")
	if rules.Len() == 0 {
		// A reused OutDir may hold an earlier build's rules for
		// redirects the app has since dropped.
		if err := removeUnder(b.OutDir, dst); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("static: remove stale _redirects: %w", err)
		}
		return nil
	}
	if err := writeUnder(b.OutDir, dst, []byte(rules.String())); err != nil {
		return err
	}
	rendered.add("_redirects")
	return nil
}

// localeTarget is a redirect target as locale tag's tree links it, by the
// rule its page links follow: a target already naming a locale, or one
// keepUnprefixed keeps (a static file, a path no route resolves), stays
// as written; any other gets tag's prefix.
func (b *Builder) localeTarget(locales i18n.PathLocales, tag, target string) string {
	p := target
	if i := strings.IndexAny(target, "?#"); i >= 0 {
		p = target[:i]
	}
	if t, _ := locales.Split(p); t != "" || b.keepUnprefixed(p) {
		return target
	}
	return locales.Path(tag, target)
}

// redirectHref is a redirect target as the export links it: the page
// form its host serves without a 301 (slashed) when the target is an
// exported page, under BasePath, query and fragment kept.
func (b *Builder) redirectHref(target string, pages map[string]bool) string {
	p, rest := target, ""
	if i := strings.IndexAny(target, "?#"); i >= 0 {
		p, rest = target[:i], target[i:]
	}
	if pages[p] || (!strings.HasSuffix(p, "/") && pages[p+"/"]) {
		p = slashed(p)
	}
	return urlBytesEscaper(b.BasePath + p + rest)
}

// urlBytesEscaper percent-encodes the bytes that would end a URL early
// where the export writes one: a control byte (a live redirect's
// Location gets the same treatment) and a space, which separates the
// fields of a _redirects line.
func urlBytesEscaper(s string) string {
	var out strings.Builder
	for i := 0; i < len(s); i++ {
		if c := s[i]; c <= 0x20 || c == 0x7f {
			fmt.Fprintf(&out, "%%%02X", c)
			continue
		}
		out.WriteByte(s[i])
	}
	return out.String()
}

// redirectPage is the document an exported redirect leaves at its old
// path. It carries no script or style: the refresh is a meta, the
// fallback a plain link, so the export's CSP holds for it like any
// page.
func redirectPage(ctx context.Context, href, policy string) string {
	lang := i18n.FromContext(ctx).Tag
	if lang == "" {
		lang = "en"
	}
	// The target lands in a meta refresh, a canonical link and an
	// anchor. HTML escaping leaves javascript: and data: intact, so the
	// scheme goes through the anchor allow-list first; a refused target
	// degrades to an inert "#".
	safe := urlsafe.CleanAnchor(href)
	if safe == "" {
		safe = "#"
	}
	h := stdhtml.EscapeString(safe)
	page := "<!doctype html>\n" +
		"<html lang=\"" + stdhtml.EscapeString(lang) + "\">\n" +
		"<head>\n" +
		"<meta charset=\"utf-8\">\n" +
		"<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n" +
		"<meta http-equiv=\"refresh\" content=\"0; url=" + h + "\">\n" +
		"<meta name=\"robots\" content=\"noindex\">\n" +
		"<link rel=\"canonical\" href=\"" + h + "\">\n" +
		"<title>" + stdhtml.EscapeString(pageRedirectTitle.Text(ctx)) + "</title>\n" +
		"</head>\n" +
		"<body>\n" +
		"<p><a href=\"" + h + "\">" + stdhtml.EscapeString(pageRedirectLink.Text(ctx)) + "</a></p>\n" +
		"</body>\n" +
		"</html>\n"
	return applyStaticCSP(page, policy)
}
