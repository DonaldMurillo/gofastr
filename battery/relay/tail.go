package relay

import (
	"fmt"
	"path"
	"strings"
)

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

func joinUpstreamPath(base, tail string) string {
	if base == "" {
		base = "/"
	}
	joined := path.Join(base, tail)
	keepSlash := strings.HasSuffix(tail, "/") || (tail == "" && strings.HasSuffix(base, "/"))
	if keepSlash && !strings.HasSuffix(joined, "/") {
		joined += "/"
	}
	return joined
}
