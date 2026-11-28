package appgen

import (
	"bytes"
	"errors"
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

func appSpecUnsafeColorNote(key, value string) string {
	err := style.ValidateColorValue(value)
	if err == nil {
		return ""
	}
	return fmt.Sprintf("\t// theme %s dropped: %s (see validateAppSpec)\n", appSpecCommentSafe(key), appSpecCommentSafe(err.Error()))
}

func appSpecCommentSafe(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\n' || r == '\r':
			b.WriteByte(' ')
		case r == '*' || r == '/' || r == '\\':
			b.WriteByte('.')
		case r < 0x20 || r == 0x7f:
		default:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

func appSpecConfiguredFonts(theme map[string]string) (body, heading string) {
	body = appSpecFontFamilyName(theme["font_body"])
	heading = appSpecFontFamilyName(theme["font_heading"])
	if heading == "" {
		heading = appSpecFontFamilyName(theme["font_display"])
	}
	return body, heading
}

const appSpecFontSysStack = "ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif"

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

func appSpecFontSlug(family string) string {
	return style.FontSlug(family)
}

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

func appSpecEffectiveStaticDir(bp appspec.AppSpec) string {
	if bp.App.StaticDir != "" {
		return bp.App.StaticDir
	}
	if len(appSpecConfiguredFontFamilies(bp.App.Theme)) > 0 {
		return "static"
	}
	if bp.App.PWA.Enabled {
		return "static"
	}
	return ""
}

// FontFetcher fetches the latin woff2 of a Google Fonts family; tests replace it.
var FontFetcher = fetchGoogleFontWoff2

var appSpecFontHTTPClient = &http.Client{Timeout: 20 * time.Second, CheckRedirect: appSpecFontRedirect}

func appSpecFontRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}
	if req.URL.Scheme != "https" || req.URL.Host != via[0].URL.Host {
		return fmt.Errorf("refusing a redirect off https://%s", via[0].URL.Host)
	}
	return nil
}

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

func appSpecFontHTTPGet(u string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
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
		bg = color.NRGBA{R: 0x4f, G: 0x46, B: 0xe5, A: 0xff}
	}
	rel := func(name string) string {
		return filepath.ToSlash(filepath.Join(dir, "icons", name))
	}
	return []GeneratedFile{
		{Name: rel("icon-192.png"), Content: string(appSpecPWAIconPNG(192, bg))},
		{Name: rel("icon-512.png"), Content: string(appSpecPWAIconPNG(512, bg))},
		{Name: rel("icon-maskable.png"), Content: string(appSpecPWAIconPNG(512, bg))},
	}
}

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
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

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

func appSpecFontRelPath(staticDir, family string) string {
	return filepath.ToSlash(filepath.Join(staticDir, "fonts", appSpecFontSlug(family)+".woff2"))
}

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

func appSpecFontFaceCSS(theme map[string]string) string {
	body, heading := appSpecConfiguredFonts(theme)
	return style.FontFaceCSS("", style.WebFont{Family: heading}, style.WebFont{Family: body})
}

func appSpecFontFaceDecl(theme map[string]string) string {
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
