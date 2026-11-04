// Package bom drops the UTF-8 byte order mark from the start of a text
// file the developer wrote. Windows editors save one (Notepad's "UTF-8
// with BOM", PowerShell 5's Out-File -Encoding utf8), and it is not
// content: YAML 1.2 section 5.2 and CSS decoding both remove it before
// parsing. A reader that keeps it glues U+FEFF onto the first token, so
// line 1 of a .env file is an invalid key, a migration's first
// directive is not a directive, and a frontmatter fence is not a fence.
//
// Every reader of a hand-written text file strips it through this
// package, and internal/bom's parity test feeds each one the same input
// with and without the mark. Round 8 found four readers that differed
// (R8-MK-01 dotenv, R8-NC-RF-05 migrate routines, R8-NC-SF-01 the
// upgrade text matcher, and core/yaml, which already stripped it).
package bom

import "strings"

// UTF8 is the encoded byte order mark, U+FEFF.
const UTF8 = "\xEF\xBB\xBF"

// Trim drops one leading byte order mark from s.
func Trim(s string) string { return strings.TrimPrefix(s, UTF8) }

// TrimBytes drops one leading byte order mark from b.
func TrimBytes(b []byte) []byte {
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		return b[3:]
	}
	return b
}
