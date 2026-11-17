package analyzers

import (
	"fmt"
	"go/ast"
	"go/token"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

// GOFASTR1417: a status UPDATE that skips the stamps its siblings write.

func ruleSettleStamp(p *contracts.Pass, rel string, _ *ast.File) []contracts.Diagnostic {
	dir := path.Dir(rel)
	v := p.Memo("settlestamp.package:"+dir, func() any {
		return settleStampPackage(p, dir)
	})
	ups, _ := v.([]*ssUpdate)
	var out []contracts.Diagnostic
	for _, u := range ups {
		if u.rel != rel || u.stamped() {
			continue
		}
		var siblings []*ssUpdate
		for _, o := range ups {
			if o != u && ecCovered(u.refs, [][]ecRef{o.refs}) {
				siblings = append(siblings, o)
			}
		}
		if len(siblings) < 2 {
			continue
		}
		stamps := map[string]bool{}
		every := true
		for _, o := range siblings {
			if !o.stamped() {
				every = false
				break
			}
			for c := range o.cols {
				if ssStampColumns[c] {
					stamps[c] = true
				}
			}
		}
		if !every {
			continue
		}
		names := make([]string, 0, len(stamps))
		for c := range stamps {
			names = append(names, c)
		}
		sort.Strings(names)
		out = append(out, diag(p, contracts.RuleSettleStamp, rel, u.pos, fmt.Sprintf(
			"this UPDATE of %s sets status and writes no stamp column, while each of its %d sibling status UPDATEs on that table writes one (%s): the row keeps an earlier transition's timestamp and error, so a reader cannot tell which path moved it — write the same stamps with a reason of its own, or annotate //gofastr:allow(GOFASTR1417) <why>",
			u.display, len(siblings), strings.Join(names, ", "))))
	}
	return out
}

// ssStampColumns record when or why a row's status last moved.
var ssStampColumns = map[string]bool{
	"updated_at": true, "modified_at": true, "changed_at": true,
	"last_updated": true, "last_error": true, "error": true,
	"error_message": true, "finished_at": true, "completed_at": true,
	"failed_at": true, "done_at": true, "settled_at": true,
	"ended_at": true, "processed_at": true, "canceled_at": true,
	"cancelled_at": true, "status_changed_at": true,
}

type ssUpdate struct {
	rel     string
	pos     token.Pos
	display string
	refs    []ecRef
	cols    map[string]bool
}

func (u *ssUpdate) stamped() bool {
	for c := range u.cols {
		if ssStampColumns[c] {
			return true
		}
	}
	return false
}

var reUpdateSet = regexp.MustCompile(`(?is)\bUPDATE\s+(?:ONLY\s+)?`)

var reSetAlias = regexp.MustCompile(`(?is)^\s*(?:(?:AS\s+)?[a-z_]\w*\s+)?SET\b`)

func settleStampPackage(p *contracts.Pass, dir string) []*ssUpdate {
	pkv := p.Memo("erasecover.package:"+dir, func() any {
		return eraseCoverPackage(p, dir)
	})
	pk, _ := pkv.(*ecPkg)
	if pk == nil {
		return nil
	}
	var out []*ssUpdate
	for _, f := range p.AppFiles() {
		if path.Dir(f.Rel) != dir {
			continue
		}
		file, ok := p.AST(f.Rel)
		if !ok {
			continue
		}
		for _, d := range file.Decls {
			fn, _ := d.(*ast.FuncDecl)
			ecEachSQLString(d, func(root ast.Expr, text string, holes, args []ast.Expr) {
				for _, m := range reUpdateSet.FindAllStringIndex(text, -1) {
					if strings.EqualFold(strings.TrimSpace(text[max(0, m[0]-3):m[0]]), "DO") {
						continue // ON CONFLICT … DO UPDATE SET
					}
					display, refs, at, ok := pk.tableNameAt(text, m[1], holes, args, fn)
					if !ok || len(refs) == 0 || strings.EqualFold(display, "set") {
						continue
					}
					head := reSetAlias.FindStringIndex(text[at:])
					if head == nil {
						continue
					}
					set := ssStatement(text[at+head[1]:])
					cols := ssSetColumns(set)
					if !cols["status"] || ssStatusUnchanged(set) {
						continue
					}
					out = append(out, &ssUpdate{rel: f.Rel, pos: ecPosIn(root, m[0]), display: display, refs: refs, cols: cols})
				}
			})
		}
	}
	return out
}

// ssSetColumns returns the columns a SET list assigns, up to the first top-level WHERE, FROM or RETURNING.
func ssSetColumns(rest string) map[string]bool {
	cols := map[string]bool{}
	depth, start := 0, 0
	flush := func(end int) {
		item := rest[start:end]
		eq := strings.IndexByte(item, '=')
		if eq < 0 {
			return
		}
		lhs := strings.ToLower(strings.TrimSpace(item[:eq]))
		lhs = reQuotedIdent.ReplaceAllString(lhs, "")
		if i := strings.LastIndexByte(lhs, '.'); i >= 0 {
			lhs = lhs[i+1:]
		}
		if lhs != "" {
			cols[lhs] = true
		}
	}
	for i := 0; i < len(rest); i++ {
		switch c := rest[i]; c {
		case '\'':
			j := i + 1
			for j < len(rest) && rest[j] != '\'' {
				j++
			}
			i = j
			continue
		case '(':
			depth++
		case ')':
			depth--
		case ';':
			if depth == 0 {
				flush(i)
				return cols
			}
		case ',':
			if depth == 0 {
				flush(i)
				start = i + 1
			}
		}
		if depth == 0 && (matchWordAt(rest, i, "where") || matchWordAt(rest, i, "from") || matchWordAt(rest, i, "returning")) {
			flush(i)
			return cols
		}
	}
	flush(len(rest))
	return cols
}

var (
	reWhereWord      = regexp.MustCompile(`(?i)\bwhere\b`)
	reSetStatusLit   = regexp.MustCompile(`(?i)(?:^|[\s,])status\s*=\s*'([^']*)'`)
	reWhereStatusLit = regexp.MustCompile(`(?is)\bwhere\b.*?\bstatus\s*=\s*'([^']*)'`)
)

var reNextStatement = regexp.MustCompile(`(?i)\b(?:UPDATE|INSERT|DELETE|SELECT|CREATE|ALTER)\s`)

// ssStatement cuts the text after SET at a semicolon or the next statement's verb; identifier quotes inside it do not end it.
func ssStatement(rest string) string {
	end := len(rest)
	if i := strings.IndexByte(rest, ';'); i >= 0 {
		end = i
	}
	depth := 0
	for i := 0; i < end; i++ {
		switch rest[i] {
		case '(':
			depth++
		case ')':
			depth--
		}
		if depth == 0 && (i == 0 || !ecIdentByte(rest[i-1])) && reNextStatement.MatchString(rest[i:min(end, i+8)]) {
			return rest[:i]
		}
	}
	return rest[:end]
}

// ssStatusUnchanged reports whether the SET writes status to the literal its own WHERE requires.
func ssStatusUnchanged(set string) bool {
	if i := strings.IndexByte(set, ';'); i >= 0 {
		set = set[:i]
	}
	head := set
	if loc := reWhereWord.FindStringIndex(set); loc != nil {
		head = set[:loc[0]]
	}
	s := reSetStatusLit.FindStringSubmatch(head)
	w := reWhereStatusLit.FindStringSubmatch(set)
	return s != nil && w != nil && strings.EqualFold(s[1], w[1])
}

func matchWordAt(s string, at int, w string) bool {
	if at+len(w) > len(s) || !strings.EqualFold(s[at:at+len(w)], w) {
		return false
	}
	if at > 0 && ecIdentByte(s[at-1]) {
		return false
	}
	return at+len(w) == len(s) || !ecIdentByte(s[at+len(w)])
}

func ecPosIn(root ast.Expr, off int) token.Pos {
	if lit, ok := root.(*ast.BasicLit); ok && strings.HasPrefix(lit.Value, "`") {
		return lit.Pos() + 1 + token.Pos(off)
	}
	return root.Pos()
}
