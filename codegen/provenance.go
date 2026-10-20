package codegen

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"strings"
)

// Kind says who owns a generated file.
type Kind string

const (
	// KindScaffold marks code a generator wrote once and the user owns
	// from then on. Editing it is expected.
	KindScaffold Kind = "scaffold"
	// KindDerived marks code a generator rebuilds from a source on every
	// run. An edit is lost on the next run.
	KindDerived Kind = "derived"
)

// markerTag opens every provenance marker, whatever the comment syntax.
const markerTag = "gofastr:generated"

// markerHeaderLines bounds where a marker may sit: the first lines of the
// file, after at most a shebang, a doctype or Dockerfile parser
// directives. A marker further down is content (a string literal, a doc
// example), never provenance.
const markerHeaderLines = 8

// Marker is the provenance line a gofastr generator writes at the top of
// a file: which generator made it, its kind, the gofastr version for a
// scaffold file, and a sum of the rest of the file. The sum is what tells
// a hand-edited file from an untouched one.
//
// A derived file carries no version: it is rebuilt by whichever CLI runs
// the build, and a version there would rewrite every derived file on each
// release with no change to its code.
type Marker struct {
	Generator string
	Kind      Kind
	Version   string
	Sum       string
}

// Sum is the content sum a marker records: the first 16 hex digits of the
// body's SHA-256. It detects edits; it is not a security property.
func Sum(body string) string {
	h := sha256.Sum256([]byte(body))
	return hex.EncodeToString(h[:8])
}

// commentStyle is how one file type spells a single-line comment.
type commentStyle struct {
	open, close string
	// blankAfter puts an empty line after the marker, so in Go the marker
	// is a comment group of its own: never the package doc comment, and
	// never moved by gofmt's doc-comment reformatting.
	blankAfter bool
	// skipLeading reports whether a header line must stay above the
	// marker (a shebang, a doctype, a Dockerfile parser directive).
	skipLeading func(line string, index int) bool
}

var (
	slashStyle = commentStyle{open: "//", blankAfter: true}
	hashStyle  = commentStyle{open: "# ", skipLeading: func(line string, i int) bool {
		return i == 0 && strings.HasPrefix(line, "#!")
	}}
	dockerStyle = commentStyle{open: "# ", skipLeading: func(line string, _ int) bool {
		return isDockerDirective(line)
	}}
	htmlStyle = commentStyle{open: "<!-- ", close: " -->", skipLeading: func(line string, i int) bool {
		return i == 0 && strings.HasPrefix(strings.ToLower(line), "<!doctype")
	}}
	cssStyle = commentStyle{open: "/* ", close: " */"}
	sqlStyle = commentStyle{open: "-- "}
)

func isDockerDirective(line string) bool {
	rest, ok := strings.CutPrefix(line, "#")
	if !ok {
		return false
	}
	key, _, ok := strings.Cut(strings.TrimSpace(rest), "=")
	if !ok {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "syntax", "escape", "check":
		return true
	}
	return false
}

// styleFor picks the comment syntax for a file by its name. A type with
// no comment syntax (JSON, go.sum, binaries) takes no marker.
func styleFor(p string) (commentStyle, bool) {
	base := path.Base(strings.ReplaceAll(p, `\`, "/"))
	switch {
	case base == "go.mod":
		return slashStyle, true
	case base == "Makefile", base == ".gitignore", base == ".dockerignore", base == ".env", strings.HasPrefix(base, ".env."):
		return hashStyle, true
	case base == "Dockerfile", strings.HasPrefix(base, "Dockerfile."), strings.HasSuffix(base, ".Dockerfile"):
		return dockerStyle, true
	}
	switch strings.ToLower(path.Ext(base)) {
	case ".go", ".js", ".mjs", ".cjs", ".ts", ".mts", ".cts":
		return slashStyle, true
	case ".css":
		return cssStyle, true
	case ".md", ".html", ".htm":
		return htmlStyle, true
	case ".yml", ".yaml", ".toml", ".sh":
		return hashStyle, true
	case ".sql":
		return sqlStyle, true
	}
	return commentStyle{}, false
}

// Stampable reports whether a file at path can carry a marker.
func Stampable(path string) bool {
	_, ok := styleFor(path)
	return ok
}

// Stamp returns content with m's marker inserted at the top, its Sum set
// from content. A file type with no comment syntax comes back unchanged.
// Every value must be a plain token, so no generator name or version can
// end the comment it sits in.
func Stamp(path, content string, m Marker) (string, error) {
	style, ok := styleFor(path)
	if !ok {
		return content, nil
	}
	m.Sum = Sum(content)
	if err := m.validate(); err != nil {
		return "", err
	}
	line := style.open + markerTag + " " + m.fields() + style.close + "\n"
	if style.blankAfter {
		line += "\n"
	}
	head, rest := splitLeading(content, style)
	return head + line + rest, nil
}

// splitLeading cuts content after the header lines that must stay above
// the marker.
func splitLeading(content string, style commentStyle) (head, rest string) {
	if style.skipLeading == nil {
		return "", content
	}
	offset := 0
	for i := 0; offset < len(content); i++ {
		line, _, found := strings.Cut(content[offset:], "\n")
		if !style.skipLeading(line, i) {
			break
		}
		offset += len(line)
		if found {
			offset++
		}
	}
	return content[:offset], content[offset:]
}

// ReadMarker finds the marker in a file's header and returns it with the
// body: the file without the marker line, the content its Sum covers.
func ReadMarker(path, content string) (Marker, string, bool) {
	style, ok := styleFor(path)
	if !ok {
		return Marker{}, "", false
	}
	prefix := style.open + markerTag + " "
	offset := 0
	for i := 0; i < markerHeaderLines && offset < len(content); i++ {
		line, _, found := strings.Cut(content[offset:], "\n")
		end := offset + len(line)
		if found {
			end++
		}
		if text, ok := strings.CutPrefix(line, prefix); ok {
			text, ok = strings.CutSuffix(text, style.close)
			if !ok {
				return Marker{}, "", false
			}
			m, ok := parseMarkerFields(text)
			if !ok {
				return Marker{}, "", false
			}
			rest := content[end:]
			if style.blankAfter {
				rest = strings.TrimPrefix(rest, "\n")
			}
			return m, content[:offset] + rest, true
		}
		offset = end
	}
	return Marker{}, "", false
}

// StripMarker returns content without its marker line, or content as it
// is when it carries none. A hash that should not change when a
// generator stamps a file (the Source hash of an owned style sheet)
// covers this.
func StripMarker(path, content string) string {
	if _, body, ok := ReadMarker(path, content); ok {
		return body
	}
	return content
}

// Edited reports whether a marked file changed since it was generated.
// ok is false when the file has no marker, so nothing can be said.
func Edited(path, content string) (edited, ok bool) {
	m, body, ok := ReadMarker(path, content)
	if !ok {
		return false, false
	}
	return Sum(body) != m.Sum, true
}

func (m Marker) fields() string {
	parts := []string{"generator=" + m.Generator, "kind=" + string(m.Kind)}
	if m.Version != "" {
		parts = append(parts, "version="+m.Version)
	}
	parts = append(parts, "sum="+m.Sum)
	return strings.Join(parts, " ")
}

func (m Marker) validate() error {
	if !isMarkerToken(m.Generator, true) {
		return fmt.Errorf("provenance marker: generator %q is not a plain name", m.Generator)
	}
	if m.Kind != KindScaffold && m.Kind != KindDerived {
		return fmt.Errorf("provenance marker: kind %q is not scaffold or derived", m.Kind)
	}
	if m.Version != "" && !isMarkerToken(m.Version, false) {
		return fmt.Errorf("provenance marker: version %q is not a plain version", m.Version)
	}
	if len(m.Sum) != 16 || !isLowerHex(m.Sum) {
		return fmt.Errorf("provenance marker: sum %q is not 16 hex digits", m.Sum)
	}
	return nil
}

func parseMarkerFields(text string) (Marker, bool) {
	var m Marker
	seen := map[string]bool{}
	for _, field := range strings.Split(text, " ") {
		key, value, ok := strings.Cut(field, "=")
		if !ok || seen[key] {
			return Marker{}, false
		}
		seen[key] = true
		switch key {
		case "generator":
			m.Generator = value
		case "kind":
			m.Kind = Kind(value)
		case "version":
			m.Version = value
		case "sum":
			m.Sum = value
		}
		// Unknown keys are skipped: a newer CLI may add one.
	}
	if m.validate() != nil {
		return Marker{}, false
	}
	return m, true
}

// isMarkerToken accepts the characters a generator name or version uses.
// Everything that could end a comment (`*/`, `-->`), start a new line or
// split a field is outside the set: `*`, `>`, `!`, spaces and control
// bytes never pass. A name may hold a slash
// (custom/reports); a version may hold a plus (v1.0.0+meta).
func isMarkerToken(s string, name bool) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
		case r >= 'A' && r <= 'Z':
		case name && r == '/':
		case !name && r == '+':
		default:
			return false
		}
	}
	return true
}

func isLowerHex(s string) bool {
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}
