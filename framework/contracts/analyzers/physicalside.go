package analyzers

import (
	"fmt"
	"go/ast"
	"go/token"
	"regexp"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/ownstyle"
	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

var (
	rePhysicalCorner = regexp.MustCompile(`^border-(top|bottom)-(left|right)-radius$`)
	rePhysicalEdge   = regexp.MustCompile(`^(margin|padding|scroll-margin|scroll-padding|border)-(left|right)(-(?:width|style|color))?$`)
	rePhysicalWord   = regexp.MustCompile(`(?:^|\s)(left|right)(?:\s|$)`)
	reFourValue      = regexp.MustCompile(`^(?:margin|padding|inset|scroll-margin|scroll-padding|border-width|border-style|border-color)$`)
	reDirOverride    = regexp.MustCompile(`:dir\((?:rtl|ltr)\)`)
	reBackgroundPos  = regexp.MustCompile(`(?i)background-position`)
)

func cssTokens(v string) []string {
	v = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(v), "!important"))
	var out []string
	depth, start := 0, -1
	for i := 0; i < len(v); i++ {
		switch c := v[i]; {
		case c == '(':
			depth++
		case c == ')':
			depth--
		case (c == ' ' || c == '\t') && depth == 0:
			if start >= 0 {
				out = append(out, v[start:i])
				start = -1
			}
			continue
		}
		if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		out = append(out, v[start:])
	}
	return out
}

func physicalSide(prop, value string, bgMirrored bool) (lit, want string, ok bool) {
	v := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(value), "!important"))
	switch prop {
	case "left", "right":
		if v == "50%" || v == "auto" || strings.HasPrefix(v, "=") || !strings.ContainsAny(v, "0123456789({") {
			return "", "", false
		}
		side := map[string]string{"left": "start", "right": "end"}[prop]
		return prop, "inset-inline-" + side, true
	case "text-align", "float", "clear":
		side := map[string]string{"left": "start", "right": "end"}[v]
		if side == "" {
			return "", "", false
		}
		if prop != "text-align" {
			side = "inline-" + side
		}
		return prop + ": " + v, prop + ": " + side, true
	case "background-position", "background-position-x":
		if bgMirrored || !rePhysicalWord.MatchString(v) {
			return "", "", false
		}
		return prop + ": " + v, "a `:dir(rtl)` rule that mirrors it", true
	}
	if m := rePhysicalEdge.FindStringSubmatch(prop); m != nil {
		side := map[string]string{"left": "start", "right": "end"}[m[2]]
		return prop, m[1] + "-inline-" + side + m[3], true
	}
	if m := rePhysicalCorner.FindStringSubmatch(prop); m != nil {
		block := map[string]string{"top": "start", "bottom": "end"}[m[1]]
		inline := map[string]string{"left": "start", "right": "end"}[m[2]]
		return prop, "border-" + block + "-" + inline + "-radius", true
	}
	if reFourValue.MatchString(prop) {
		if t := cssTokens(v); len(t) == 4 && t[1] != t[3] {
			return prop + ": " + v, prop + "-block and " + prop + "-inline", true
		}
		return "", "", false
	}
	if prop == "border-radius" && !strings.Contains(v, "/") {
		t := cssTokens(v)
		switch {
		case len(t) == 2 && t[0] != t[1],
			len(t) == 3 && (t[0] != t[1] || t[2] != t[1]),
			len(t) == 4 && (t[0] != t[1] || t[2] != t[3]):
			return prop + ": " + v, "the border-start-start/start-end/end-end/end-start-radius longhands", true
		}
	}
	return "", "", false
}

func checkGoPhysicalSides(p *contracts.Pass, rel string, body []byte, lines []string) []contracts.Diagnostic {
	file, ok := p.AST(rel)
	if !ok {
		return nil
	}
	positions := p.FileSet().File(file.Pos())
	css := make([]byte, len(body))
	for i, c := range body {
		css[i] = ' '
		if c == '\n' {
			css[i] = c
		}
	}
	for node := range ast.Preorder(file) {
		if lit, ok := node.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			start, end := positions.Offset(lit.Pos()), positions.Offset(lit.End())
			copy(css[start:end], body[start:end])
		}
	}
	return scanPhysicalSides(rel, strings.Split(stripComments(string(css)), "\n"), lines, physicalBuilderPairs(file, positions))
}

func physicalBuilderPairs(file *ast.File, positions *token.File) map[int][]cssDeclaration {
	type builderCall struct {
		scope ast.Node
		list  ast.Node
		call  *ast.CallExpr
		recv  ast.Expr
		first int
	}
	type receiver struct {
		scope ast.Node
		name  string
	}
	type ruleCall struct {
		end  token.Pos
		list ast.Node
		dir  bool
	}
	ruleReceivers := make(map[receiver][]ruleCall)
	var calls []builderCall
	ast.PreorderStack(file, nil, func(node ast.Node, stack []ast.Node) bool {
		recv, method, call, ok := selectorCall(node)
		if !ok {
			return true
		}
		scope := cssBuilderScope(file, stack)
		var first int
		switch method {
		case "Rule":
			if id, ok := recv.(*ast.Ident); ok {
				key := receiver{scope, id.Name}
				list := builderStatementList(call, stack)
				ruleReceivers[key] = append(ruleReceivers[key], ruleCall{call.End(), list, dirSelectorArg(call, false)})
			}
			return true
		case "Set":
		case "Pseudo", "Child":
			first = 1
		default:
			return true
		}
		calls = append(calls, builderCall{scope, builderStatementList(call, stack), call, recv, first})
		return true
	})
	pairs := make(map[int][]cssDeclaration)
	for _, candidate := range calls {
		inDir, chained := styleRuleChain(candidate.recv)
		if id, isIdent := candidate.recv.(*ast.Ident); isIdent && !chained {
			rules, known := ruleReceivers[receiver{candidate.scope, id.Name}]
			if !known {
				continue
			}
			var latest token.Pos
			for _, rule := range rules {
				if rule.end <= candidate.call.Pos() && rule.end > latest {
					latest = rule.end
					inDir = rule.dir && rule.list != nil && rule.list == candidate.list
				}
			}
		} else if !chained {
			continue
		}
		if candidate.first == 1 {
			inDir = dirSelectorArg(candidate.call, inDir)
		}
		if inDir {
			continue
		}
		args := candidate.call.Args
		for i := candidate.first; i+1 < len(args); i += 2 {
			name, nameOK := stringLit(args[i])
			value, valueOK := stringLit(args[i+1])
			if nameOK && valueOK {
				line := positions.PositionFor(args[i].Pos(), false).Line
				pairs[line] = append(pairs[line], cssDeclaration{property: strings.ToLower(name), value: value})
			}
		}
	}
	return pairs
}

func builderStatementList(call *ast.CallExpr, stack []ast.Node) ast.Node {
	var child ast.Node = call
	for i := len(stack) - 1; i > 0; i-- {
		switch parent := stack[i].(type) {
		case *ast.SelectorExpr:
			if parent.X != child {
				return nil
			}
		case *ast.CallExpr:
			if parent.Fun != child {
				return nil
			}
		case *ast.ParenExpr:
		case *ast.ExprStmt:
			switch stack[i-1].(type) {
			case *ast.BlockStmt, *ast.CaseClause, *ast.CommClause:
				return stack[i-1]
			}
			return nil
		default:
			return nil
		}
		child = stack[i]
	}
	return nil
}

func dirSelectorArg(call *ast.CallExpr, inherited bool) bool {
	if len(call.Args) == 0 {
		return false
	}
	selector, ok := stringLit(call.Args[0])
	if !ok || (!inherited && !reDirOverride.MatchString(selector)) {
		return false
	}
	tokens := ownstyle.Tokenize(selector)
	depth, directional := 0, inherited
	for i, t := range tokens {
		switch t.Type {
		case ownstyle.TokenFunction, ownstyle.TokenOpenParen, ownstyle.TokenOpenSquare:
			if depth == 0 && t.Text == "dir(" && i > 0 && i+2 < len(tokens) &&
				tokens[i-1].Type == ownstyle.TokenColon && tokens[i-1].End == t.Start &&
				(tokens[i+1].Text == "rtl" || tokens[i+1].Text == "ltr") &&
				tokens[i+2].Type == ownstyle.TokenCloseParen {
				directional = true
			}
			depth++
		case ownstyle.TokenCloseParen, ownstyle.TokenCloseSquare:
			depth--
		case ownstyle.TokenComma:
			if depth == 0 {
				if !directional {
					return false
				}
				directional = false
			}
		}
	}
	return directional && depth == 0
}

// styleRuleChain reports whether recv chains back to a Rule call and
// whether that Rule's selector is a :dir() override.
func styleRuleChain(recv ast.Expr) (inDir, ok bool) {
	for {
		next, method, call, isCall := selectorCall(recv)
		if !isCall {
			return false, false
		}
		switch method {
		case "Rule":
			return dirSelectorArg(call, false), true
		case "Set", "Pseudo", "Child", "End":
			recv = next
		default:
			return false, false
		}
	}
}

func cssBuilderScope(file *ast.File, stack []ast.Node) ast.Node {
	for i := len(stack) - 1; i >= 0; i-- {
		switch stack[i].(type) {
		case *ast.FuncDecl, *ast.FuncLit:
			return stack[i]
		}
	}
	return file
}

func scanPhysicalSides(rel string, scan, lines []string, pairs map[int][]cssDeclaration) []contracts.Diagnostic {
	var out []contracts.Diagnostic
	bgMirrored := mirrorsBackground(scan)
	var dir dirBlock
	for i, line := range scan {
		inDir := dir.line(line)
		decls := appendCSSDeclarations(nil, line, ":", reDeclCSS)
		out = append(out, checkPhysicalSides(rel, i+1, decls, lines[i], bgMirrored, inDir)...)
		out = append(out, checkPhysicalSides(rel, i+1, pairs[i+1], lines[i], bgMirrored, nil)...)
	}
	return out
}

func checkPhysicalSides(rel string, lineNo int, decls []cssDeclaration, orig string, bgMirrored bool, inDir []bool) []contracts.Diagnostic {
	var out []contracts.Diagnostic
	for _, decl := range decls {
		if decl.offset < len(inDir) && inDir[decl.offset] {
			continue
		}
		lit, want, ok := physicalSide(decl.property, decl.value, bgMirrored)
		if !ok {
			continue
		}
		out = append(out, contracts.Diagnostic{
			RuleID: contracts.RulePhysicalSide,
			File:   rel,
			Line:   lineNo,
			Message: fmt.Sprintf("%s pins a physical side, which stays put under <html dir=\"rtl\">; write %s so the kit mirrors for right-to-left readers",
				lit, want),
			Snippet: strings.TrimSpace(orig),
		})
	}
	return out
}

func mirrorsBackground(scan []string) bool {
	dir := dirBlock{rtlOnly: true}
	for _, line := range scan {
		flags := dir.line(line)
		if flags == nil {
			continue
		}
		for _, match := range reBackgroundPos.FindAllStringIndex(line, -1) {
			if flags[match[0]] {
				return true
			}
		}
	}
	return false
}

type dirBlock struct {
	depth   int
	pending bool // a :dir() selector seen, its '{' not yet
	rtlOnly bool
}

func (d *dirBlock) line(s string) []bool {
	if d.depth == 0 && !d.pending && !strings.Contains(s, ":dir(") {
		return nil // the common line: no block to open or close
	}
	in := make([]bool, len(s))
	sel := reDirOverride.FindAllStringIndex(s, -1)
	if d.rtlOnly {
		filtered := sel[:0]
		for _, match := range sel {
			if s[match[0]:match[1]] == ":dir(rtl)" {
				filtered = append(filtered, match)
			}
		}
		sel = filtered
	}
	open := d.depth > 0 || d.pending
	for i := range len(s) {
		if len(sel) > 0 && i == sel[0][0] {
			open = true
			if d.depth == 0 {
				d.pending = true
			}
			sel = sel[1:]
		}
		in[i] = open
		switch s[i] {
		case '{':
			if d.depth > 0 || d.pending {
				d.depth++
				d.pending = false
			}
		case '}':
			d.depth = max(d.depth-1, 0)
		}
		open = d.depth > 0 || d.pending
	}
	return in
}
