package static

import (
	"fmt"
	"path/filepath"
	"strings"
)

const staticExportCSP = "default-src 'self'; img-src 'self' data:; object-src 'none'; worker-src 'self'; " +
	"form-action 'self'; frame-ancestors 'none'; base-uri 'none'; " +
	"require-trusted-types-for 'script'; trusted-types gofastr"

func (b *Builder) policy() string {
	if p := strings.TrimSpace(b.ContentSecurityPolicy); p != "" {
		return p
	}
	return staticExportCSP
}

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

var metaIgnored = map[string]bool{"frame-ancestors": true, "report-uri": true, "sandbox": true}

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

func writeHeadersFile(outDir, policy string) error {
	body := "/*\n" +
		"  Content-Security-Policy: " + policy + "\n" +
		"  X-Content-Type-Options: nosniff\n" +
		"  Referrer-Policy: strict-origin-when-cross-origin\n"
	return writeUnder(outDir, filepath.Join(outDir, "_headers"), []byte(body))
}
