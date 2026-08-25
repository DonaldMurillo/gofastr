package relay

import (
	"fmt"
	"path"
	"strings"
)

// validateTail is the sink-side guard on the only attacker-influenced
// part of a relay request: the subtree remainder under a route prefix.
//
// The framework's router already bounds path params (core/router.Param
// truncates at CR/LF/NUL, single-segment slashes, and dot-dot
// segments), and Go's ServeMux cleans literal traversal and duplicate
// slashes out of the request path before matching. Neither replaces
// validation at the sink: encoded forms ("%2e%2e", "%2f", "%23", "%5c")
// survive both layers decoded into the tail, and PathValue-based
// truncation would silently proxy a DIFFERENT path than the one the
// client asked for. This validator rejects instead of truncating.
//
// rawEncoded reports whether r.URL.RawPath is non-empty, i.e. some
// part of the request URL was percent-encoded in a way that does not
// round-trip as the canonical encoding of the decoded value (how
// "%2F" and "%2e" smuggling present). Any such encoding in the tail
// region is refused outright: legit vendor asset paths are plain, and
// the canonical encodings that DO round-trip (UTF-8, "%20") leave
// RawPath empty and pass.
func validateTail(tail string, rawEncoded bool) error {
	if rawEncoded {
		return fmt.Errorf("path tail uses non-canonical percent-encoding (possible %%2F/%%2e smuggling)")
	}
	if tail == "" {
		return nil
	}
	for i := range len(tail) {
		c := tail[i]
		if c < 0x20 || c == 0x7f {
			return fmt.Errorf("path tail contains a control character")
		}
	}
	if strings.Contains(tail, "\\") {
		return fmt.Errorf(`path tail contains a backslash`)
	}
	if strings.Contains(tail, "#") {
		return fmt.Errorf("path tail contains a fragment marker")
	}
	segs := strings.Split(tail, "/")
	for i, seg := range segs {
		if seg == "" {
			// A single trailing empty segment is a trailing slash, which
			// vendor SDKs legitimately use (posthog-js posts /i/v0/e/).
			// Empty segments anywhere else are the "//" smuggling shape.
			if i == len(segs)-1 && i > 0 {
				continue
			}
			return fmt.Errorf("path tail contains an empty segment")
		}
		if seg == "." || seg == ".." {
			return fmt.Errorf("path tail contains a traversal segment")
		}
	}
	return nil
}

// joinUpstreamPath maps a validated tail onto the upstream's base
// path. path.Join also lexically cleans the result: with the tail
// validator in front it is a no-op, but it guarantees that even a
// smuggled dot-segment could only resolve INSIDE the upstream's base
// path, never into another origin.
func joinUpstreamPath(base, tail string) string {
	if base == "" {
		base = "/"
	}
	joined := path.Join(base, tail)
	// path.Join drops a trailing slash; the upstream may distinguish
	// /e from /e/ (or redirect one to the other, which the relay
	// refuses), so a validated trailing slash is preserved verbatim.
	if strings.HasSuffix(tail, "/") && !strings.HasSuffix(joined, "/") {
		joined += "/"
	}
	return joined
}
