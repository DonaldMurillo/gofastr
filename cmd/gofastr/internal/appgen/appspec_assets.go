package appgen

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

// appSpecFontFamilyName extracts the primary family name from a theme font
// value like "Hanken Grotesk" (or a full stack the author wrote), stripping
// quotes and any trailing fallback so it can be turned into a Google Fonts query.
// appSpecFontFamilyName extracts the first family from a font stack
// and reduces it to characters that are safe inside the emitted CSS
// string literal.
//
// Trimming quotes only at the edges was not enough: a comma-free value
// whose quote sits in the INTERIOR passed through untouched and closed
// the `font-family: '…'` literal, letting spec text append arbitrary
// rules to the app-wide stylesheet (which is fed to both
// uihost.WithCustomCSS and the admin battery). Real Google Fonts
// families are exactly this character set, so the allow-list costs
// nothing legitimate.
func appSpecFontFamilyName(v string) string {
	v = strings.TrimSpace(v)
	if i := strings.IndexByte(v, ','); i >= 0 {
		v = v[:i]
	}
	v = strings.Trim(strings.TrimSpace(v), "'\"")
	var b strings.Builder
	for _, r := range v {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

// appSpecUnsafeColorNote is the emitter-side backstop for theme color values.
// validateAppSpec is the loud gate (it fails generation and names the token),
// but the emitter is reachable from `loadAppSpecPath(path, false)` and from a
// hand-built App spec, and a theme value that reaches CSS unchecked is a
// stylesheet-injection sink. When the value fails style.ValidateColorValue this
// returns a generated comment to emit INSTEAD of the assignment, so the token
// falls back to the DefaultTheme value.
//
// Dropping is deliberate: the value cannot be sanitized into a color, and a
// comment in the generated source is visible to whoever reads app.go, unlike a
// silent skip. The empty string means "safe, emit the assignment".
func appSpecUnsafeColorNote(key, value string) string {
	err := style.ValidateColorValue(value)
	if err == nil {
		return ""
	}
	// The rejected value never enters the comment; a value carrying "*/"
	// would close it and the rest would become live Go.
	return fmt.Sprintf("\t// theme %s dropped: %s (see validateAppSpec)\n", appSpecCommentSafe(key), appSpecCommentSafe(err.Error()))
}

// appSpecCommentSafe reduces s to bytes that cannot terminate or escape the
// single-line Go comment it is interpolated into.
func appSpecCommentSafe(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\n' || r == '\r':
			b.WriteByte(' ')
		case r == '*' || r == '/' || r == '\\':
			b.WriteByte('.')
		case r < 0x20 || r == 0x7f:
			// dropped
		default:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

// appSpecConfiguredFonts returns the heading + body family names declared in
// the app spec theme (font_heading / font_display alias, font_body).
func appSpecConfiguredFonts(theme map[string]string) (body, heading string) {
	body = appSpecFontFamilyName(theme["font_body"])
	heading = appSpecFontFamilyName(theme["font_heading"])
	if heading == "" {
		heading = appSpecFontFamilyName(theme["font_display"])
	}
	return body, heading
}

const appSpecFontSysStack = "ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif"

// appSpecFontStacks builds the full CSS font-family stacks for the theme's
// body and heading tokens, appending a standard system font fallback (and
// letting the heading fall back through the body family first).
func appSpecFontStacks(theme map[string]string) (bodyStack, headingStack string) {
	body, heading := appSpecConfiguredFonts(theme)
	if body != "" {
		bodyStack = fmt.Sprintf("'%s', %s", body, appSpecFontSysStack)
	}
	if heading != "" {
		if body != "" {
			headingStack = fmt.Sprintf("'%s', '%s', %s", heading, body, appSpecFontSysStack)
		} else {
			headingStack = fmt.Sprintf("'%s', %s", heading, appSpecFontSysStack)
		}
	}
	return bodyStack, headingStack
}

// appSpecFontSlug turns a font family name into the self-hosted file slug,
// e.g. "Bricolage Grotesque" -> "bricolage-grotesque".
// It delegates to style.FontSlug so the slug in the emitted `src: url(…)`
// and the slug in the woff2 path the developer is told to write to are
// produced by ONE function. They were two implementations that agreed on
// space-separated names and diverged on anything with punctuation; a
// family like "Libre Baskerville, Display" would have been served from a
// path nothing wrote to, with no error anywhere.
func appSpecFontSlug(family string) string {
	return style.FontSlug(family)
}

// appSpecConfiguredFontFamilies returns the theme's declared font families in
// @font-face order (heading first, then body), de-duplicated. Empty when the
// theme names no fonts.
func appSpecConfiguredFontFamilies(theme map[string]string) []string {
	body, heading := appSpecConfiguredFonts(theme)
	var out []string
	seen := map[string]bool{}
	for _, f := range []string{heading, body} {
		if f == "" || seen[f] {
			continue
		}
		seen[f] = true
		out = append(out, f)
	}
	return out
}

// appSpecEffectiveStaticDir is the directory the generated app serves static
// files from. It's the app spec's static_dir when set; otherwise "static" when
// the theme declares self-hosted fonts (so the emitted woff2 files have a home
// and a serving route). Empty only when neither applies.
func appSpecEffectiveStaticDir(bp appspec.AppSpec) string {
	if bp.App.StaticDir != "" {
		return bp.App.StaticDir
	}
	if len(appSpecConfiguredFontFamilies(bp.App.Theme)) > 0 {
		return "static"
	}
	// A PWA needs a home for its scaffolded icons (and a serving route
	// for them), same as self-hosted fonts.
	if bp.App.PWA.Enabled {
		return "static"
	}
	return ""
}

// FontFetcher resolves a Google Fonts family name to a self-hostable woff2 file
// (the regular-weight latin subset). It's a package var so the test suite can
// stub it; the real implementation makes network calls, which tests never do.
var FontFetcher = fetchGoogleFontWoff2

// appSpecFontHTTPClient bounds the generate-time font fetch so an unreachable
// or slow CDN degrades to the offline warning path instead of hanging generate.
var appSpecFontHTTPClient = &http.Client{Timeout: 20 * time.Second}

// fetchGoogleFontWoff2 downloads the latin woff2 subset for a Google Fonts
// family. It queries the Google Fonts CSS API with a modern User-Agent (so the
// API returns woff2, not ttf), extracts the first gstatic .woff2 URL (preferring
// the latin subset), and downloads it. Any network/parse failure returns an
// error; generation then falls back to a loud warning rather than a silent 404.
func fetchGoogleFontWoff2(family string) ([]byte, error) {
	q := url.Values{}
	q.Set("family", family+":wght@400;700")
	q.Set("display", "swap")
	cssURL := "https://fonts.googleapis.com/css2?" + q.Encode()
	css, err := appSpecFontHTTPGet(cssURL)
	if err != nil {
		return nil, err
	}
	woffURL := appSpecFirstWoff2URL(string(css))
	if woffURL == "" {
		return nil, fmt.Errorf("no woff2 url in Google Fonts response for %q", family)
	}
	return appSpecFontHTTPGet(woffURL)
}

// appSpecFontHTTPGet fetches a URL with a browser-like User-Agent and returns
// its body, erroring on any non-2xx status.
func appSpecFontHTTPGet(u string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	// The CSS API serves woff2 only to modern browsers; identify as one.
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36")
	resp, err := appSpecFontHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("GET %s: status %d", u, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
}

// appSpecFirstWoff2URL pulls the first gstatic .woff2 URL out of a Google
// Fonts css2 response, preferring the block tagged `/* latin */`.
//
// The returned URL is handed straight to appSpecFontHTTPGet, so the fetch
// target is chosen by the RESPONSE BODY, not by us. The comment above has always
// said "gstatic"; the code only checked for ".woff2" anywhere in the string, so
// a hijacked or redirected css2 response could point the fetch at any host it
// liked. The app spec author cannot reach this (the `family` query param is
// reduced to [A-Za-z0-9 _-] first), which is why it is a contract gap rather
// than a app-spec-driven SSRF, closed by making the code enforce what the
// comment claims.
func appSpecFirstWoff2URL(css string) string {
	extract := func(s string) string {
		i := strings.Index(s, "url(")
		if i < 0 {
			return ""
		}
		s = s[i+len("url("):]
		j := strings.Index(s, ")")
		if j < 0 {
			return ""
		}
		u := strings.Trim(strings.TrimSpace(s[:j]), "'\"")
		if appSpecIsGstaticWoff2(u) {
			return u
		}
		return ""
	}
	if i := strings.Index(css, "/* latin */"); i >= 0 {
		if u := extract(css[i:]); u != "" {
			return u
		}
	}
	// Fall back to the first woff2 anywhere in the response.
	for rest := css; ; {
		i := strings.Index(rest, "url(")
		if i < 0 {
			return ""
		}
		if u := extract(rest[i:]); u != "" {
			return u
		}
		rest = rest[i+len("url("):]
	}
}

// appSpecIsGstaticWoff2 reports whether u is an https fonts.gstatic.com URL
// whose path ends in .woff2: the only thing generation is willing to fetch out
// of a Google Fonts css2 response.
//
// Parsed, not prefix-matched: a `strings.HasPrefix(u, "https://fonts.gstatic.com")`
// test also accepts https://fonts.gstatic.com.attacker.test/x.woff2. And the
// ".woff2" test is on the PATH's suffix, not on the whole URL, so it cannot be
// satisfied by a query string or fragment (…/evil?x=.woff2).
//
// If Google ever moves the font CDN, generation reports the family in `missing`
// and warns; the app still builds, minus the self-hosted font.
func appSpecIsGstaticWoff2(u string) bool {
	parsed, err := url.Parse(u)
	if err != nil {
		return false
	}
	if parsed.Scheme != "https" || parsed.Host != "fonts.gstatic.com" {
		return false
	}
	return strings.HasSuffix(parsed.Path, ".woff2")
}

// appSpecFontAssets fetches every configured font family and returns the
// woff2 files to emit under <static>/fonts/. Families whose fetch fails are
// reported in `missing` (as their emitted relative path) so the caller can warn;
// generation still proceeds so an offline run produces a working app minus the
// custom fonts.
func appSpecFontAssets(bp appspec.AppSpec) (files []GeneratedFile, missing []string) {
	dir := appSpecEffectiveStaticDir(bp)
	for _, fam := range appSpecConfiguredFontFamilies(bp.App.Theme) {
		rel := appSpecFontRelPath(dir, fam)
		data, err := FontFetcher(fam)
		if err != nil || len(data) == 0 {
			missing = append(missing, rel)
			continue
		}
		files = append(files, GeneratedFile{Name: rel, Content: string(data)})
	}
	return files, missing
}

// appSpecPWAOptionLiteral renders the uihost.WithPWA(...) call for the
// generated main.go. Only author-declared fields are emitted (so pack
// recovers exactly the authored app spec); the framework applies the
// defaults at serve time. The icon set always points at the scaffolded
// placeholder files.
func appSpecPWAOptionLiteral(bp appspec.AppSpec) string {
	p := bp.App.PWA
	var fields []string
	if p.Name != "" {
		fields = append(fields, fmt.Sprintf("Name: %q", p.Name))
	}
	if p.ShortName != "" {
		fields = append(fields, fmt.Sprintf("ShortName: %q", p.ShortName))
	}
	if p.Description != "" {
		fields = append(fields, fmt.Sprintf("Description: %q", p.Description))
	}
	if p.StartURL != "" {
		fields = append(fields, fmt.Sprintf("StartURL: %q", p.StartURL))
	}
	if p.Scope != "" {
		fields = append(fields, fmt.Sprintf("Scope: %q", p.Scope))
	}
	if p.Display != "" {
		fields = append(fields, "Display: "+appSpecPWADisplayConst(p.Display))
	}
	if p.ThemeColor != "" {
		fields = append(fields, fmt.Sprintf("ThemeColor: %q", p.ThemeColor))
	}
	if p.BackgroundColor != "" {
		fields = append(fields, fmt.Sprintf("BackgroundColor: %q", p.BackgroundColor))
	}
	// The framework's built-in deny list covers the default /api and
	// /auth mounts; a custom api_prefix or auth base_path must follow
	// its app's real mounts or the never-precache/never-intercept
	// guarantee silently stops covering the API.
	var deny []string
	if prefix := strings.Trim(bp.App.APIPrefix, "/"); prefix != "" && prefix != "api" {
		deny = append(deny, "/"+prefix)
	}
	if bp.App.Auth.Enabled {
		if base := appSpecAuthBase(bp); base != "/auth" {
			deny = append(deny, base)
		}
	}
	if len(deny) > 0 {
		quoted := make([]string, len(deny))
		for i, d := range deny {
			quoted[i] = fmt.Sprintf("%q", d)
		}
		fields = append(fields, "DenyPaths: []string{"+strings.Join(quoted, ", ")+"}")
	}
	fields = append(fields, `Icons: []uihost.PWAIcon{{Src: "/icons/icon-192.png", Sizes: "192x192", Type: "image/png"}, {Src: "/icons/icon-512.png", Sizes: "512x512", Type: "image/png"}, {Src: "/icons/icon-maskable.png", Sizes: "512x512", Type: "image/png", Purpose: uihost.PWAIconPurposeMaskable}}`)
	return "uihost.WithPWA(uihost.PWAConfig{" + strings.Join(fields, ", ") + "})"
}

// appSpecPWADisplayConst maps a validated app spec display mode to
// the typed uihost constant emitted into generated code.
func appSpecPWADisplayConst(display string) string {
	switch display {
	case "fullscreen":
		return "uihost.PWADisplayFullscreen"
	case "minimal-ui":
		return "uihost.PWADisplayMinimalUI"
	case "browser":
		return "uihost.PWADisplayBrowser"
	default:
		return "uihost.PWADisplayStandalone"
	}
}

// appSpecPWAIconAssets emits the replaceable placeholder icons the app
// generator scaffolds for app.pwa: 192px and 512px "any" icons plus a 512px
// maskable variant, colored from the declared theme_color (falling back
// to the theme's primary token, then the framework indigo). Generated
// in-process. Deterministic PNG bytes, no network fetch.
func appSpecPWAIconAssets(bp appspec.AppSpec) []GeneratedFile {
	if !bp.App.PWA.Enabled {
		return nil
	}
	dir := appSpecEffectiveStaticDir(bp)
	fill := bp.App.PWA.ThemeColor
	if fill == "" {
		fill = bp.App.Theme["primary"]
	}
	bg, ok := appSpecParseHexColor(fill)
	if !ok {
		bg = color.NRGBA{R: 0x4f, G: 0x46, B: 0xe5, A: 0xff} // indigo fallback
	}
	rel := func(name string) string {
		return filepath.ToSlash(filepath.Join(dir, "icons", name))
	}
	return []GeneratedFile{
		{Name: rel("icon-192.png"), Content: string(appSpecPWAIconPNG(192, bg))},
		{Name: rel("icon-512.png"), Content: string(appSpecPWAIconPNG(512, bg))},
		// The maskable icon uses the same full-bleed art: a solid
		// background with a centered dot keeps all content inside the
		// mask safe zone regardless of the platform's crop shape.
		{Name: rel("icon-maskable.png"), Content: string(appSpecPWAIconPNG(512, bg))},
	}
}

// appSpecPWAIconPNG draws the placeholder icon: a solid brand-color
// square with a centered translucent-white dot, well inside the
// maskable safe zone.
func appSpecPWAIconPNG(size int, bg color.NRGBA) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	dot := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xd9}
	c := float64(size) / 2
	r := float64(size) * 0.22
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx, dy := float64(x)+0.5-c, float64(y)+0.5-c
			if dx*dx+dy*dy <= r*r {
				img.SetNRGBA(x, y, dot)
			} else {
				img.SetNRGBA(x, y, bg)
			}
		}
	}
	var buf bytes.Buffer
	// Encode cannot fail on an in-memory buffer with a valid image.
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

// appSpecParseHexColor parses #rgb / #rrggbb into an opaque color.
func appSpecParseHexColor(s string) (color.NRGBA, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	switch len(s) {
	case 3:
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	case 6:
	default:
		return color.NRGBA{}, false
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return color.NRGBA{}, false
	}
	return color.NRGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xff}, true
}

// appSpecFontRelPath is the emitted path for a family's woff2 file, matching
// the /fonts/<slug>.woff2 URL in fontFaceCSS (the static dir is served at root).
func appSpecFontRelPath(staticDir, family string) string {
	return filepath.ToSlash(filepath.Join(staticDir, "fonts", appSpecFontSlug(family)+".woff2"))
}

// appSpecMissingFontSlugs reports which configured font files are absent from
// the rendered file set (e.g. an offline fetch dropped them) so generate can
// warn with the exact paths the user must supply.
func appSpecMissingFontSlugs(bp appspec.AppSpec, files []GeneratedFile) []string {
	fams := appSpecConfiguredFontFamilies(bp.App.Theme)
	if len(fams) == 0 {
		return nil
	}
	present := make(map[string]bool, len(files))
	for _, f := range files {
		present[f.Name] = true
	}
	dir := appSpecEffectiveStaticDir(bp)
	var missing []string
	for _, fam := range fams {
		if rel := appSpecFontRelPath(dir, fam); !present[rel] {
			missing = append(missing, rel)
		}
	}
	return missing
}

// appSpecFontFaceCSS returns the @font-face rules for the theme's declared
// families, self-hosted from <static>/fonts/<slug>.woff2. This is the SINGLE
// font-loading source; it's prepended to the UI host's stylesheet AND handed to
// the admin battery, so every surface (marketing, app, back-office) loads the
// exact same fonts with no external CDN dependency. Drop the matching woff2 into
// <static_dir>/fonts/. Returns "" when the theme declares no fonts.
// The rule itself is emitted by core-ui/style, not spelled out here: a
// generator that writes its own `@font-face` string owns a second
// styling surface, which is the drift AGENTS.md hard rule 7 is about.
// `gofastr verify` flags this exact shape (GOFASTR1801), and
// style.FontFaceCSS is the upstream primitive that finding asked for.
func appSpecFontFaceCSS(theme map[string]string) string {
	body, heading := appSpecConfiguredFonts(theme)
	// Heading first, then body: the declaration order callers rely on.
	return style.FontFaceCSS("", style.WebFont{Family: heading}, style.WebFont{Family: body})
}

// appSpecFontFaceDecl emits the generated app's fontFaceCSS declaration.
//
// It emits a CALL to style.FontFaceCSS rather than a baked CSS string.
// Emitting the string would move the violation rather than fix it: the
// generator would stop shipping CSS only by writing the CSS into every
// app it generates, where `gofastr verify` would correctly flag it and
// the developer could do nothing about it. Composing the design system
// is the shape hard rule 7 asks for, and the generated file already
// imports core-ui/style for its theme.
func appSpecFontFaceDecl(theme map[string]string) string {
	// Emitted even when no fonts are declared: main.go references
	// fontFaceCSS unconditionally, so omitting the declaration does not
	// produce a smaller app, it produces one that does not compile. With
	// no fonts the call returns "".
	families := appSpecConfiguredFontFamilies(theme)
	var fonts strings.Builder
	for _, f := range families {
		fmt.Fprintf(&fonts, ", style.WebFont{Family: %q}", f)
	}
	return fmt.Sprintf(
		"// fontFaceCSS holds the @font-face rules for the app's fonts, shared by\n"+
			"// the UI host and the admin battery so every surface loads identical fonts.\n"+
			"// Drop the matching woff2 files into <static>/fonts/.\n"+
			"var fontFaceCSS = style.FontFaceCSS(\"\"%s)\n\n", fonts.String())
}
