package analyzers

import (
	"fmt"
	"go/ast"
	"go/types"
	"regexp"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

// GOFASTR1418: a hand-written primary key that does not say NOT NULL.
func ruleNullableKey(p *contracts.Pass, rel string, file *ast.File) []contracts.Diagnostic {
	var out []contracts.Diagnostic
	for _, d := range file.Decls {
		ecEachSQLString(d, func(root ast.Expr, text string, holes, args []ast.Expr) {
			for _, m := range reCreateTableINE.FindAllStringIndex(text, -1) {
				name, open, ok := nkTableName(text, m[1])
				if !ok {
					continue
				}
				body, ok := ecColumnList(text, open)
				if !ok {
					continue
				}
				bad := nkNullableKeys(text, open+1, body, holes, args)
				if len(bad) == 0 {
					continue
				}
				out = append(out, diag(p, contracts.RuleNullableKey, rel, ecPosIn(root, m[0]), fmt.Sprintf(
					"table %s declares primary key column %s without NOT NULL: SQLite stores a NULL there, any number of times, where Postgres refuses the row — write NOT NULL on every key column (only a lone INTEGER PRIMARY KEY, the rowid, and a SERIAL key fill themselves), or annotate //gofastr:allow(GOFASTR1418) <why>",
					name, strings.Join(bad, ", "))))
			}
		})
	}
	return out
}

var (
	reInlineKey  = regexp.MustCompile(`(?i)\bPRIMARY\s+KEY\b`)
	reTableKey   = regexp.MustCompile(`(?is)^(?:CONSTRAINT\s+\S+\s+)?PRIMARY\s+KEY\s*\(([^)]*)\)`)
	reNotNull    = regexp.MustCompile(`(?i)\bNOT\s+NULL\b`)
	reSelfFilled = regexp.MustCompile(`(?i)^\s*(?:INTEGER|(?:SMALL|BIG)?SERIAL)\b`)
)

func nkTableName(text string, at int) (string, int, bool) {
	open := strings.IndexByte(text[at:], '(')
	if open < 0 {
		return "", 0, false
	}
	open += at
	name := strings.TrimSpace(text[at:open])
	if name == "" || len(strings.Fields(name)) != 1 {
		return "", 0, false
	}
	if strings.ContainsAny(name, "%"+ecHole) {
		name = "(a table named at run time)"
	}
	return name, open, true
}

// nkNullableKeys names the key columns of a column list that lack NOT NULL; base is the list's offset in text.
func nkNullableKeys(text string, base int, body string, holes, args []ast.Expr) []string {
	type column struct{ shown, def string }
	defs := map[string]column{}
	var tableKey []string
	var bad []string
	start := 0
	for _, item := range dcSplitTop(body) {
		at := base + start + len(item) - len(strings.TrimLeft(item, " \t\r\n"))
		start += len(item) + 1
		item = strings.TrimSpace(item)
		if sm := reTableKey.FindStringSubmatchIndex(item); sm != nil {
			for _, c := range nkSplitOffsets(item[sm[2]:sm[3]]) {
				id, _ := nkIdent(text, at+sm[2]+c.off, c.tok, holes, args)
				tableKey = append(tableKey, id)
			}
			continue
		}
		fs := strings.Fields(item)
		if len(fs) == 0 {
			continue
		}
		switch strings.ToLower(fs[0]) {
		case "constraint", "primary", "check", "exclude", "unique", "foreign":
			continue
		}
		id, shown := nkIdent(text, at, fs[0], holes, args)
		def := item[len(fs[0]):]
		defs[id] = column{shown, def}
		if reInlineKey.MatchString(def) && !reNotNull.MatchString(def) && !reSelfFilled.MatchString(def) {
			bad = append(bad, shown)
		}
	}
	for _, id := range tableKey {
		c, ok := defs[id]
		switch {
		case !ok:
			bad = append(bad, strings.TrimPrefix(id, "name:")+" (no column of that name is declared in the statement)")
		case !reNotNull.MatchString(c.def):
			bad = append(bad, c.shown)
		}
	}
	return bad
}

type nkTok struct {
	tok string
	off int
}

func nkSplitOffsets(list string) []nkTok {
	var out []nkTok
	off := 0
	for _, it := range strings.Split(list, ",") {
		if f := strings.Fields(it); len(f) > 0 {
			out = append(out, nkTok{f[0], off + strings.Index(it, f[0])})
		}
		off += len(it) + 1
	}
	return out
}

// nkIdent gives a column token one identity: its folded name, or the source behind a verb or hole.
func nkIdent(text string, at int, tok string, holes, args []ast.Expr) (id, shown string) {
	switch {
	case strings.HasPrefix(tok, "%"):
		if idx, _ := ecVerbIndex(text, at); idx >= 0 && idx < len(args) {
			src := types.ExprString(args[idx])
			return "expr:" + src, src
		}
	case strings.HasPrefix(tok, ecHole):
		if end := strings.Index(tok[1:], ecHole); end >= 0 {
			if n, err := strconv.Atoi(tok[1 : 1+end]); err == nil && n < len(holes) {
				src := types.ExprString(holes[n])
				return "expr:" + src, src
			}
		}
	default:
		name := strings.ToLower(strings.Trim(tok, "\"`[]"))
		return "name:" + name, name
	}
	return "slot:" + strconv.Itoa(at), "a column named at run time"
}
