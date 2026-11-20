package check

import (
	"go/ast"
	"go/token"
	"regexp"
	"strings"
)

var reAllowMarker = regexp.MustCompile(`^(?://|/\*)\s*gofastr:allow\(([A-Za-z0-9_,\s]+)\)(.*)$`)

type markerComment struct {
	text      string
	line, end int // 1-based lines of its first and last byte
	alone     bool
}

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
		if strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(rest), "-—:")) == "" {
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
