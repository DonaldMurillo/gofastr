package check

import (
	"go/ast"
	"go/token"
	"regexp"
	"strings"
)

// The //gofastr:allow(<name>) <why> marker, read with the grammar of
// internal/analyzers/allow (that package needs go/analysis, which this
// one must not import). A marker is live only as the FIRST thing in a
// comment: a marker quoted in a string, or in prose partway through a
// comment, is documentation. The reason is the comment's own text after
// the parenthesis (less a block comment's closing delimiter) and must
// not be empty; code after a block comment is not a reason. A trailing
// marker covers its own line; a marker alone on its line covers that
// line and the next one.
//
// The runtimeprose and layerprefix lints used to find the marker with
// strings.Index anywhere on the line or the line above, so a trailing
// marker silenced the next line too, and code after it counted as the
// reason.
var reAllowMarker = regexp.MustCompile(`^(?://|/\*)\s*gofastr:allow\(([A-Za-z0-9_,\s]+)\)(.*)$`)

// markerComment is one comment and where it sits.
type markerComment struct {
	text      string
	line, end int // 1-based lines of its first and last byte
	alone     bool
}

// allowedLines returns the lines a marker naming name covers.
func allowedLines(comments []markerComment, name string) map[int]bool {
	out := map[int]bool{}
	for _, c := range comments {
		m := reAllowMarker.FindStringSubmatch(c.text)
		if m == nil || !markerNames(m[1], name) {
			continue
		}
		rest := m[2]
		if strings.HasPrefix(c.text, "/*") {
			rest = strings.TrimSuffix(strings.TrimSpace(rest), "*/")
		}
		if strings.TrimSpace(rest) == "" {
			continue
		}
		out[c.line] = true
		if c.alone {
			out[c.end+1] = true
		}
	}
	return out
}

func markerNames(list, name string) bool {
	for n := range strings.SplitSeq(list, ",") {
		if strings.TrimSpace(n) == name {
			return true
		}
	}
	return false
}

// jsComments returns the comments of a JS source. Code blanks every
// comment byte to a space and keeps everything else, so a comment starts
// wherever the two views first disagree.
func jsComments(f jsSource) []markerComment {
	var out []markerComment
	for i := 0; i < len(f.Src) && i < len(f.Code); i++ {
		if f.Src[i] == f.Code[i] {
			continue
		}
		end := len(f.Src)
		if strings.HasPrefix(f.Src[i:], "/*") {
			if j := strings.Index(f.Src[i+2:], "*/"); j >= 0 {
				end = i + 2 + j + 2
			}
		} else if j := strings.IndexByte(f.Src[i:], '\n'); j >= 0 {
			end = i + j
		}
		out = append(out, markerComment{
			text:  f.Src[i:end],
			line:  f.lineOf(i),
			end:   f.lineOf(end),
			alone: aloneOnLine(f.Src, i, end),
		})
		i = end
	}
	return out
}

// goComments returns the comments of a parsed Go file.
func goComments(fset *token.FileSet, file *ast.File, src []byte) []markerComment {
	var out []markerComment
	for _, cg := range file.Comments {
		for _, c := range cg.List {
			pos, end := fset.Position(c.Pos()), fset.Position(c.End())
			out = append(out, markerComment{
				text:  c.Text,
				line:  pos.Line,
				end:   end.Line,
				alone: aloneOnLine(string(src), pos.Offset, end.Offset),
			})
		}
	}
	return out
}

// aloneOnLine reports whether src[start:end] has only whitespace before
// it on its first line and after it on its last.
func aloneOnLine(src string, start, end int) bool {
	if start > len(src) || end > len(src) {
		return false
	}
	ls := strings.LastIndexByte(src[:start], '\n') + 1
	if strings.TrimSpace(src[ls:start]) != "" {
		return false
	}
	le := len(src)
	if j := strings.IndexByte(src[end:], '\n'); j >= 0 {
		le = end + j
	}
	return strings.TrimSpace(src[end:le]) == ""
}
