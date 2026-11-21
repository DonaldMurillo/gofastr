// Package mountpath validates literal mount paths and canonicalizes path prefixes.
package mountpath

import (
	"fmt"
	"path"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// NormalizePrefix canonicalizes a path-only prefix and represents the root as empty.
func NormalizePrefix(prefix string) string {
	if prefix == "" {
		return ""
	}
	prefix = textsafe.SanitizeControlBytes(prefix)
	prefix = strings.ReplaceAll(prefix, "\\", "/")
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	prefix = path.Clean(prefix)
	if prefix == "/" {
		return ""
	}
	return prefix
}

// reservedGoFastrSegments are the path segments directly under
// /__gofastr that the framework and its batteries already serve
// (runtime.js, app.css, the SSE bus, compute workers, …); mounting
// onto one collides at registration time.
var reservedGoFastrSegments = map[string]bool{
	"sse": true, "session": true, "action": true, "widgets": true,
	"widget": true, "embed": true, "embed.js": true, "embed-runtime.js": true,
	"runtime.js": true, "runtime": true, "app.css": true, "comp": true,
	"color-scheme.js": true, "compute": true, "pwa": true, "icons": true,
}

// Validate enforces the mount contract for what (e.g. "Config.Path"):
// a literal absolute path, clean, and clear of the reserved /__gofastr
// namespace. The caller prefixes its battery name onto the error text.
func Validate(what, p string) error {
	if !strings.HasPrefix(p, "/") {
		return fmt.Errorf("%s %q must be absolute (start with /)", what, p)
	}
	if p == "/" {
		return fmt.Errorf("%s %q would capture the whole app", what, p)
	}
	if strings.HasSuffix(p, "/") {
		return fmt.Errorf("%s %q must not end with a slash", what, p)
	}
	if err := ValidateSegments(what, p); err != nil {
		return err
	}
	if p == "/__gofastr" {
		return fmt.Errorf("%s %q is the framework's reserved namespace root; mount under a subpath like %q or a custom path", what, p, "/__gofastr/t")
	}
	if rest, ok := strings.CutPrefix(p, "/__gofastr/"); ok && rest != "" {
		seg, _, _ := strings.Cut(rest, "/")
		if reservedGoFastrSegments[seg] {
			return fmt.Errorf("%s %q collides with the reserved framework route /__gofastr/%s", what, p, seg)
		}
	}
	return nil
}

// ValidateSegments carries the per-character and per-segment rules of
// Validate (control bytes, metacharacters, encoding, traversal) for an
// already-absolute p; relay applies it to route prefixes.
func ValidateSegments(what, p string) error {
	for i := range len(p) {
		c := p[i]
		if c <= 0x20 || c == 0x7f {
			return fmt.Errorf("%s %q contains a control character or space", what, p)
		}
		if c == '{' || c == '}' || c == '$' {
			return fmt.Errorf("%s %q must be a literal path without ServeMux metacharacters", what, p)
		}
		if c == '%' || c == '#' || c == '?' || c == '\\' {
			return fmt.Errorf("%s %q contains %q; percent-encoding and fragments are not valid here", what, p, string(c))
		}
	}
	for seg := range strings.SplitSeq(p[1:], "/") {
		if seg == "" {
			return fmt.Errorf("%s %q contains an empty path segment", what, p)
		}
		if seg == "." || seg == ".." {
			return fmt.Errorf("%s %q contains a traversal segment", what, p)
		}
	}
	return nil
}
