package static

import (
	"fmt"
	"path/filepath"
	"strings"
)

// staticExportCSP is the Content-Security-Policy a static export carries
// with it.
//
// Every browser gadget the runtime can be pushed into, an injected
// data-behavior <script src>, a signal-driven URL attribute, a fetch
// aimed off-origin, is mitigated in production by the default
// `default-src 'self'` that core/middleware.SecurityHeaders sets as a
// response HEADER. A static export is a directory of files: S3, GitHub
// Pages and friends set no such header, so the export was the one
// deployment target where the mitigation simply was not there.
//
// It mirrors the served default and adds the two directives the audit
// recommended for both: object-src 'none' (plugin content is a
// script-execution surface with no legitimate use here) and form-action
// 'self' (an injected form cannot post the page's data elsewhere).
const staticExportCSP = "default-src 'self'; img-src 'self' data:; object-src 'none'; " +
	"form-action 'self'; frame-ancestors 'none'; base-uri 'self'"

// policy is the Content-Security-Policy this build ships: the
// configured one, or the built-in default.
func (b *Builder) policy() string {
	if p := strings.TrimSpace(b.ContentSecurityPolicy); p != "" {
		return p
	}
	return staticExportCSP
}

// checkExportCSP refuses a policy the export cannot write safely. A
// nonce source is per response on a server; baked into files every
// visitor downloads, it is a published value that any injected script
// can carry, and the export links every script, so none needs one. A
// control byte would end the _headers line and start a header (or a
// path block) of the policy's choosing. A quote or angle bracket is no
// CSP token (RFC 9110 field values and CSP's grammar keep them out) and
// would end the <meta> attribute.
func checkExportCSP(policy string) error {
	for i := 0; i < len(policy); i++ {
		if c := policy[i]; (c < 0x20 && c != '\t') || c == 0x7f {
			return fmt.Errorf("static: Content-Security-Policy contains control byte %#x; a header value is one line", c)
		}
	}
	if strings.ContainsAny(policy, "\"<>") {
		return fmt.Errorf("static: Content-Security-Policy contains a quote or angle bracket, which no CSP source uses: %q", policy)
	}
	for src := range strings.FieldsSeq(strings.NewReplacer(";", " ", ",", " ").Replace(policy)) {
		if strings.HasPrefix(strings.ToLower(src), "'nonce-") {
			return fmt.Errorf("static: Content-Security-Policy source %s is a nonce: a static file gives every visitor the same one, so it protects nothing; drop it (exported pages link every script)", src)
		}
	}
	return nil
}

// metaIgnored names the directives a <meta>-delivered policy ignores
// (CSP3 §3.1, "The <meta> element"): browsers log a console warning for
// each and enforce nothing, so the meta leaves them out. _headers keeps
// them.
var metaIgnored = map[string]bool{"frame-ancestors": true, "report-uri": true, "sandbox": true}

// metaPolicies splits a header value into the policies a <meta> can
// carry: one per comma-separated policy, since a <meta> parses a single
// one, with the directives a meta ignores dropped. A policy left with no
// directive is dropped.
func metaPolicies(policy string) []string {
	var out []string
	for part := range strings.SplitSeq(policy, ",") {
		var kept []string
		for d := range strings.SplitSeq(part, ";") {
			d = strings.Join(strings.Fields(d), " ")
			if d == "" {
				continue
			}
			name, _, _ := strings.Cut(d, " ")
			if metaIgnored[strings.ToLower(name)] {
				continue
			}
			kept = append(kept, d)
		}
		if len(kept) > 0 {
			out = append(out, strings.Join(kept, "; "))
		}
	}
	return out
}

// applyStaticCSP inserts the policy as in-document metas, immediately
// after <head> so they precede anything that can trigger a fetch. A
// page that already carries one (the print shell, say) is left alone.
// policy has passed checkExportCSP, so it holds no quote or bracket to
// escape; & is escaped all the same.
func applyStaticCSP(page, policy string) string {
	if strings.Contains(page, "http-equiv=\"Content-Security-Policy\"") {
		return page
	}
	var meta strings.Builder
	for _, p := range metaPolicies(policy) {
		meta.WriteString("\n<meta http-equiv=\"Content-Security-Policy\" content=\"" + strings.ReplaceAll(p, "&", "&amp;") + "\">")
	}
	return insertAfterHead(page, meta.String())
}

// insertAfterHead puts s right after the document's opening <head> tag.
func insertAfterHead(page, s string) string {
	if s == "" {
		return page
	}
	if i := strings.Index(page, "<head"); i >= 0 {
		if j := strings.IndexByte(page[i:], '>'); j >= 0 {
			at := i + j + 1
			return page[:at] + s + page[at:]
		}
	}
	return page
}

// writeHeadersFile emits a Netlify / Cloudflare Pages `_headers` file so
// the policy arrives as a REAL header on hosts that read one. Both ship
// because neither alone covers the field, but they do NOT cover the same
// directives: per CSP L3 §3.1 a <meta>-delivered policy ignores
// frame-ancestors (as well as report-uri and sandbox), so the meta covers
// the fetch directives everywhere while the clickjacking guard lands only
// on hosts that read this file.
func writeHeadersFile(outDir, policy string) error {
	body := "/*\n" +
		"  Content-Security-Policy: " + policy + "\n" +
		"  X-Content-Type-Options: nosniff\n" +
		"  Referrer-Policy: strict-origin-when-cross-origin\n"
	return writeUnder(outDir, filepath.Join(outDir, "_headers"), []byte(body))
}
