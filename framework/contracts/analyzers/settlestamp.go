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

// ----------------------------------------------------------------------
// GOFASTR1417: a status UPDATE that skips the stamps its siblings write.
// ----------------------------------------------------------------------

// Bug class: a table whose status transitions all record WHEN and WHY
// (updated_at, last_error, finished_at …) and one transition that sets
// status alone. The row then lists with the timestamp and error of an
// earlier transition, and nothing tells the reader which path moved it.
// 04ac2e5e6: battery/queue's deadLetterExpiredFinalClaims swept a claim
// whose lease expired on its final attempt with `UPDATE … SET
// status='failed'` and nothing else, while claim, Ack, Nack, release and
// Replay all wrote updated_at (and Nack last_error), so ListJobs("failed")
// showed the claim's updated_at and an earlier attempt's error, and a
// worker that died holding the claim read as a handler error.
//
// Shape, per package: every `UPDATE <table> [AS alias] SET …` statement
// (a Sprintf format, a plain literal, or a concatenation, the strings
// GOFASTR1415 reads) whose SET list assigns status. Statements are
// grouped by table identity, resolved the way GOFASTR1415 resolves a
// table name (a literal, a constant, a receiver field, a quoting
// wrapper, a method returning the name: `q.qt()` in every DBQueue
// method is one table), with dialectdrift's normalization of quoting,
// placeholders and aliases applied to the assigned columns. A status
// UPDATE that assigns none of the stamp columns (updated_at, modified_at,
// changed_at, last_updated, last_error, error, error_message,
// finished_at, completed_at, failed_at, done_at, settled_at, ended_at,
// processed_at, canceled_at, cancelled_at, status_changed_at) reports
// when the table has at least two other status UPDATEs in the package
// and every one of them assigns a stamp. The finding names the stamps
// the siblings write.
//
// Fix spelling: write the stamps the sibling transitions write, with a
// reason a reader can tell apart (the queue's leaseExpiredError). A
// transition that must keep the earlier stamp on purpose says so with
// //gofastr:allow(GOFASTR1417) <why>.
//
// Deliberately silent on:
//   - tables where any sibling status UPDATE writes no stamp either: no
//     convention to break, and the rule does not invent one;
//   - a table with fewer than two other status UPDATEs: one sibling is
//     not a convention;
//   - a status written to the literal its own WHERE requires (SET
//     status='pending' … WHERE … status='pending'): the row does not
//     change state, so there is no transition to stamp. The outbox's
//     no-handler requeue re-leases a pending delivery that way, and is
//     neither a finding nor a sibling;
//   - UPDATEs that do not assign status (a heartbeat, a lease
//     extension, a payload rewrite), and ON CONFLICT … DO UPDATE SET,
//     which is an upsert arm, not a transition;
//   - a status UPDATE whose table name cannot be resolved: it joins no
//     group rather than a wrong one;
//   - _test.go and generated files (AppFiles already excludes both);
//   - any site annotated //gofastr:allow(GOFASTR1417) <why>.
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

// reUpdateSet is the head of an UPDATE up to its table name; the name
// itself is read by tableNameAt, so a verb slot or an operand resolves
// like GOFASTR1415's DDL. ON CONFLICT … DO UPDATE SET has no table and
// is refused by the name check.
var reUpdateSet = regexp.MustCompile(`(?is)\bUPDATE\s+(?:ONLY\s+)?`)

// reSetAlias is what may sit between the table name and SET: an alias.
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

// ssSetColumns returns the columns a SET list assigns, reading up to the
// first top-level WHERE / FROM / RETURNING or the statement's end.
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

// ssStatement cuts the text after SET at the statement's end: a
// semicolon, a quote that closes a literal quoted inside the string (SQL
// held in Go source text), or the next statement's verb outside
// parentheses. A WHERE read past it would borrow the next statement's
// predicates.
func ssStatement(rest string) string {
	end := len(rest)
	if i := strings.IndexAny(rest, ";`\""); i >= 0 {
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

// ssStatusUnchanged reports whether the SET writes status to the literal
// its own WHERE already requires (SET status='pending' … WHERE
// status='pending'): the row does not change state, so there is no
// transition to stamp (framework/outbox's no-handler requeue, which
// re-leases a pending delivery).
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

// matchWordAt reports whether the word w (any case) starts at s[at] as a
// whole word.
func matchWordAt(s string, at int, w string) bool {
	if at+len(w) > len(s) || !strings.EqualFold(s[at:at+len(w)], w) {
		return false
	}
	if at > 0 && ecIdentByte(s[at-1]) {
		return false
	}
	return at+len(w) == len(s) || !ecIdentByte(s[at+len(w)])
}

// ecPosIn is the position of byte off of a string expression's text: the
// exact byte inside a raw literal, the expression's start otherwise.
func ecPosIn(root ast.Expr, off int) token.Pos {
	if lit, ok := root.(*ast.BasicLit); ok && strings.HasPrefix(lit.Value, "`") {
		return lit.Pos() + 1 + token.Pos(off)
	}
	return root.Pos()
}
