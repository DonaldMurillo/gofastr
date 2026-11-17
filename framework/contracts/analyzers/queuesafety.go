package analyzers

import (
	"fmt"
	"go/ast"
	"go/token"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
	"github.com/DonaldMurillo/gofastr/internal/asciicase"
)

// GOFASTR1408: absolute retry-counter writes in UPDATE SET.

func ruleAbsoluteAttempts(p *contracts.Pass, rel string, file *ast.File) []contracts.Diagnostic {
	var out []contracts.Diagnostic
	for _, stmt := range sqlStatements(file) {
		if stmt.verb != "update" {
			continue
		}
		for _, m := range reCounterAssign.FindAllStringSubmatchIndex(stmt.set, -1) {
			lo, hi := m[2], m[3] // the quoted spelling's group
			if lo < 0 {
				lo, hi = m[4], m[5] // the bare spelling's group
			}
			col := stmt.set[lo:hi]
			rhs := setValue(stmt.set[m[1]:])
			if (!reHostValue.MatchString(rhs) && !(stmt.formatted && formattedHostValue(rhs))) || selfReference(rhs, col) {
				continue
			}
			d := diag(p, contracts.RuleAbsoluteAttempts, rel,
				stmt.at(stmt.setOff+m[0]),
				fmt.Sprintf("UPDATE SET writes the retry counter from a host value (%s), not the relative form: overlapping claimants each write back their own snapshot and under-count — increment in SQL (attempts = attempts + 1, the battery/queue claim spelling) or move the count out of the settle write",
					strings.TrimSpace(stmt.set[m[0]:m[1]+len(rhs)])))
			out = append(out, d)
		}
	}
	return out
}

var (
	reCounterAssign = regexp.MustCompile(`(?i)(?:"(attempts|retries|tries|failures)"|\b(attempts|retries|tries|failures)\b)\s*=`)
	reHostValue     = regexp.MustCompile(`\$\d+|\?`)
	reFormatValue   = regexp.MustCompile(`%%|%(?:\[\d+\])?[#0-9.]*[vTtbcdoOxXUeEfFgGspq]`)
)

func formattedHostValue(value string) bool {
	for _, verb := range reFormatValue.FindAllString(value, -1) {
		if verb != "%%" {
			return true
		}
	}
	return false
}

// setValue returns one SET assignment's value: the text up to the next top-level comma.
func setValue(rest string) string {
	depth := 0
	for i, r := range rest {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth <= 0 {
				return rest[:i]
			}
		}
	}
	return rest
}

// selfReference reports whether a SET value reads the column it assigns, as a whole word.
func selfReference(value, col string) bool {
	value, col = asciicase.Lower(value), asciicase.Lower(col)
	for from := 0; ; {
		i := strings.Index(value[from:], col)
		if i < 0 {
			return false
		}
		start, end := from+i, from+i+len(col)
		if (start == 0 || !ecIdentByte(value[start-1])) && (end == len(value) || !ecIdentByte(value[end])) {
			return true
		}
		from = end
	}
}

// GOFASTR1409: unfenced claim-state writes on a token-fenced table.

func ruleUnfencedClaim(p *contracts.Pass, rel string, file *ast.File) []contracts.Diagnostic {
	dir := path.Dir(rel)
	armed, _ := p.Memo("unfencedclaim.package:"+dir, func() any {
		return unfencedClaimArmed(p, dir)
	}).(bool)
	if !armed {
		return nil
	}
	var out []contracts.Diagnostic
	for _, stmt := range sqlStatements(file) {
		switch {
		case stmt.upsert, // an INSERT's conflict arm writes the row it inserts
			reTokenFence.MatchString(stmt.where),
			reTokenUnowned.MatchString(stmt.where),
			reTokenMint.MatchString(stmt.set), // the claim mints its token
			reStaleClaimAt.MatchString(stmt.where),
			reStaleClaimAtRev.MatchString(stmt.where),
			reTerminalStatus.MatchString(stmt.where):
			continue
		}
		if !reClaimVocab.MatchString(stmt.set) &&
			!reClaimVocab.MatchString(stmt.where) &&
			!reClaimedStatus.MatchString(stmt.where) {
			continue
		}
		d := diag(p, contracts.RuleUnfencedClaim, rel, stmt.at(stmt.verbOff),
			fmt.Sprintf("%s touches claim state with no fence of its own (no token predicate, no claimed_at staleness bound, no terminal status guard): a stale claimant can mutate the re-claimant's live row — fence it on the claim token the way Ack/Nack do (battery/queue db.go)",
				strings.ToUpper(stmt.verb[:1])+stmt.verb[1:]))
		out = append(out, d)
	}
	return out
}

func unfencedClaimArmed(p *contracts.Pass, dir string) bool {
	for _, f := range p.AppFiles() {
		if path.Dir(f.Rel) != dir {
			continue
		}
		file, ok := p.AST(f.Rel)
		if !ok {
			continue
		}
		for _, stmt := range sqlStatements(file) {
			if reTokenFence.MatchString(stmt.where) {
				return true
			}
		}
	}
	return false
}

var (
	reTokenFence      = regexp.MustCompile(`(?i)\b(?:claim|lease|owner)_token\s*(?:=|IS\s+NOT\s+DISTINCT\s+FROM)\s*(?:\$\d+|\?|%[sdv])`)
	reTokenUnowned    = regexp.MustCompile(`(?i)\b(?:claim|lease|owner)_token\s+IS\s+NULL\b`)
	reTokenMint       = regexp.MustCompile(`(?i)\b(?:claim|lease|owner)_token\s*=\s*(?:\$\d+|\?|%[sdv])`)
	reStaleClaimAt    = regexp.MustCompile(`(?i)\b(?:claimed_at|claimed_until|lease_until|lease_expires_at)\s*(?:<=|<)`)
	reStaleClaimAtRev = regexp.MustCompile(`(?i)(?:>=|>)\s*(?:claimed_at|claimed_until|lease_until|lease_expires_at)\b`)
	reTerminalStatus  = regexp.MustCompile(`(?i)\bstatus\b[^;]*'(?:failed|dead|abandoned|dispatched|completed|done|succeeded|cancelled|canceled|expired)'`)
	reClaimedStatus   = regexp.MustCompile(`(?i)\bstatus\b[^;]*'claimed'`)
	reClaimVocab      = regexp.MustCompile(`(?i)\b(?:claimed_at|claimed_until|claim_token|lease_token|owner_token|lease_until|lease_expires_at|max_attempts|attempts)\b`)
)

// sqlStmt is one UPDATE or DELETE inside a string literal, with its clauses and source geometry.
type sqlStmt struct {
	verb      string // "update" / "delete", lowercased
	verbOff   int    // offset of the verb within text
	text      string // the statement, from the unquoted literal value
	set       string // the SET clause ("" when absent)
	setOff    int    // offset of the SET clause within text
	where     string // the WHERE clause ("" when absent)
	upsert    bool   // the UPDATE is an INSERT's ON CONFLICT … DO UPDATE arm
	valOff    int    // offset of text within the literal's value
	lit       *ast.BasicLit
	raw       bool // backtick literal: value bytes are source bytes
	formatted bool
}

func (s sqlStmt) at(off int) token.Pos {
	if !s.raw {
		return s.lit.Pos() + 1 + token.Pos(min(off, len(s.text)))
	}
	return s.lit.Pos() + 1 + token.Pos(s.valOff+min(off, len(s.text)))
}

var (
	reUpsertArm = regexp.MustCompile(`(?i)\bDO\s+$`)
	reSQLVerb   = regexp.MustCompile(`(?i)\b(UPDATE|DELETE)\b`)
	reSQLSet    = regexp.MustCompile(`(?i)\bSET\b`)
	reSQLWhre   = regexp.MustCompile(`(?i)\bWHERE\b`)
)

// sqlStatements returns every UPDATE/DELETE in the file's string literals, split on ';'; a literal is formatted when a Sprintf format names it, directly or through a const or var.
func sqlStatements(file *ast.File) []sqlStmt {
	var out []sqlStmt
	formatted := map[*ast.BasicLit]bool{}
	aliases := importAliases(file)
	bound := map[string][]ast.Expr{}
	ast.Inspect(file, func(node ast.Node) bool {
		switch decl := node.(type) {
		case *ast.GenDecl:
			var last []ast.Expr
			for _, spec := range decl.Specs {
				value, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				values := value.Values
				if decl.Tok == token.CONST && len(values) == 0 && value.Type == nil {
					values = last
				}
				last = values
				for i, name := range value.Names {
					if i < len(values) {
						bound[name.Name] = append(bound[name.Name], values[i])
					}
				}
			}
		case *ast.AssignStmt:
			if len(decl.Lhs) == len(decl.Rhs) {
				for i, left := range decl.Lhs {
					if name, ok := left.(*ast.Ident); ok {
						bound[name.Name] = append(bound[name.Name], decl.Rhs[i])
					}
				}
			}
		}
		return true
	})
	var markFormat func(ast.Expr, map[string]bool)
	markFormat = func(expr ast.Expr, seen map[string]bool) {
		ast.Inspect(expr, func(node ast.Node) bool {
			switch node := node.(type) {
			case *ast.BasicLit:
				if node.Kind == token.STRING {
					formatted[node] = true
				}
			case *ast.Ident:
				if !seen[node.Name] {
					seen[node.Name] = true
					for _, value := range bound[node.Name] {
						markFormat(value, seen)
					}
				}
			}
			return true
		})
	}
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := qualifiedCall(node, aliases, "fmt", "Sprintf")
		if ok && len(call.Args) > 0 {
			markFormat(call.Args[0], map[string]bool{})
		}
		return true
	})
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		value, err := strconv.Unquote(lit.Value)
		if err != nil {
			return true
		}
		if !reSQLVerb.MatchString(value) {
			return true
		}
		raw := strings.HasPrefix(lit.Value, "`")
		valOff := 0
		for _, chunk := range strings.Split(value, ";") {
			m := reSQLVerb.FindStringSubmatchIndex(chunk)
			if m == nil {
				valOff += len(chunk) + 1
				continue
			}
			stmt := sqlStmt{
				verb:      strings.ToLower(chunk[m[2]:m[3]]),
				verbOff:   m[2],
				text:      chunk,
				valOff:    valOff,
				lit:       lit,
				raw:       raw,
				formatted: formatted[lit],
			}
			stmt.upsert = stmt.verb == "update" && reUpsertArm.MatchString(chunk[:m[2]])
			stmt.set, stmt.setOff = clause(chunk, reSQLSet, reSQLWhre)
			stmt.where, _ = clause(chunk, reSQLWhre, nil)
			out = append(out, stmt)
			valOff += len(chunk) + 1
		}
		return true
	})
	return out
}

func clause(text string, start, end *regexp.Regexp) (string, int) {
	loc := start.FindStringIndex(text)
	if loc == nil {
		return "", 0
	}
	if end != nil {
		if e := end.FindStringIndex(text[loc[1]:]); e != nil {
			return text[loc[0] : loc[1]+e[0]], loc[0]
		}
	}
	return text[loc[0]:], loc[0]
}
