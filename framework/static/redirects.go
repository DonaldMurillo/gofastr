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
			if prev, dup := owner[foldPath(rel)]; dup {
				return fmt.Errorf("static: pages %q and %q both export to %s; give one of them another path", prev, from, rel)
			}
			owner[foldPath(rel)] = from
			dst := filepath.Join(b.OutDir, filepath.FromSlash(rel))
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

func redirectPage(ctx context.Context, href, policy string) string {
	lang := i18n.FromContext(ctx).Tag
	if lang == "" {
		lang = "en"
	}
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
