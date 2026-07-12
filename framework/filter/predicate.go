package filter

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// Predicate is a node in a boolean filter tree: either a LEAF (one
// field/op/value comparison) or a GROUP (AND/OR of child predicates).
// It is the parsed, validated form of a `?where=<json>` request — every
// field has already been checked against the entity's schema allow-list
// and every operator against the supported set, so [BuildPredicate] may
// interpolate field names trusting they are safe while binding all
// values as placeholders.
//
// Groups let callers express nested boolean logic the flat query-param
// filters cannot — e.g. `status = A OR (priority = high AND assignee =
// me)`. The whole tree compiles to ONE parenthesized WHERE clause that
// the query builder AND-composes with the framework's owner/tenant/
// soft-delete scopes; a user OR-group can never widen past those scopes
// because each is a separate, individually-parenthesized clause.
type Predicate struct {
	// Leaf fields (Children == nil):
	Field  string
	Op     FilterOp
	Value  string   // for scalar ops
	Values []string // for OpIn

	// Group fields (Children != nil):
	Or       bool // false = AND, true = OR
	Children []Predicate
}

func (p *Predicate) isGroup() bool { return len(p.Children) > 0 }

// Predicate-tree bounds. A hostile `?where=` could otherwise nest
// thousands of nodes to blow up parse time and statement size. Both are
// fail-closed: exceeding either rejects the whole tree with an error
// (mirrors ParseSort's fail-closed cap, not ParseFilters' silent skip).
const (
	maxPredicateDepth = 8
	maxPredicateNodes = 64
)

// whereOps maps the JSON operator token to the internal FilterOp. Only
// these are accepted; an unknown op rejects the tree. A leaf with no op
// defaults to equality.
var whereOps = map[string]FilterOp{
	"eq":   OpEq,
	"gt":   OpGt,
	"lt":   OpLt,
	"gte":  OpGte,
	"lte":  OpLte,
	"like": OpLike,
	"in":   OpIn,
}

// rawPred is the wire shape of one `?where=` node. Exactly one of
// {and, or, field} is expected per node.
type rawPred struct {
	And    []json.RawMessage `json:"and"`
	Or     []json.RawMessage `json:"or"`
	Field  string            `json:"field"`
	Op     string            `json:"op"`
	Value  string            `json:"value"`
	Values []string          `json:"values"`
}

// ParseWhere parses a `?where=<json>` predicate tree and validates every
// leaf field against the schema allow-list (Hidden fields excluded, same
// value-disclosure-oracle rationale as ParseFilters) and every operator
// against the supported set. Returns (nil, nil) when raw is empty. Any
// unknown field, unknown operator, malformed JSON, empty/ambiguous node,
// or a tree that exceeds the depth/node bounds returns an error — the
// caller maps it to 400. On success the tree is safe for BuildPredicate
// to compile.
func ParseWhere(raw string, fields []schema.Field) (*Predicate, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	allow := make(map[string]bool, len(fields))
	for _, f := range fields {
		if f.Hidden {
			continue
		}
		allow[f.Name] = true
	}
	var msg json.RawMessage
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		return nil, fmt.Errorf("where: invalid JSON: %w", err)
	}
	count := 0
	p, err := parseNode(msg, allow, 1, &count)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func parseNode(msg json.RawMessage, allow map[string]bool, depth int, count *int) (Predicate, error) {
	if depth > maxPredicateDepth {
		return Predicate{}, fmt.Errorf("where: nesting exceeds max depth %d", maxPredicateDepth)
	}
	*count++
	if *count > maxPredicateNodes {
		return Predicate{}, fmt.Errorf("where: exceeds max node count %d", maxPredicateNodes)
	}
	var rp rawPred
	if err := json.Unmarshal(msg, &rp); err != nil {
		return Predicate{}, fmt.Errorf("where: invalid node: %w", err)
	}

	hasAnd, hasOr, hasField := len(rp.And) > 0, len(rp.Or) > 0, rp.Field != ""
	// Exactly one kind per node.
	if btoi(hasAnd)+btoi(hasOr)+btoi(hasField) != 1 {
		return Predicate{}, fmt.Errorf("where: each node must be exactly one of and/or/field")
	}

	if hasAnd || hasOr {
		kids := rp.And
		or := false
		if hasOr {
			kids, or = rp.Or, true
		}
		children := make([]Predicate, 0, len(kids))
		for _, k := range kids {
			c, err := parseNode(k, allow, depth+1, count)
			if err != nil {
				return Predicate{}, err
			}
			children = append(children, c)
		}
		return Predicate{Or: or, Children: children}, nil
	}

	// Leaf.
	if !allow[rp.Field] {
		// Unknown or Hidden field — never build a predicate on it.
		return Predicate{}, fmt.Errorf("where: unknown filter field %q", rp.Field)
	}
	opTok := rp.Op
	if opTok == "" {
		opTok = "eq"
	}
	op, ok := whereOps[opTok]
	if !ok {
		return Predicate{}, fmt.Errorf("where: unknown operator %q", rp.Op)
	}
	leaf := Predicate{Field: rp.Field, Op: op}
	if op == OpIn {
		vals := rp.Values
		if len(vals) == 0 && rp.Value != "" {
			vals = strings.Split(rp.Value, ",")
		}
		if len(vals) == 0 {
			return Predicate{}, fmt.Errorf("where: %q with op in requires values", rp.Field)
		}
		if len(vals) > maxINListEntries {
			return Predicate{}, fmt.Errorf("where: in-list exceeds %d entries", maxINListEntries)
		}
		leaf.Values = vals
	} else {
		leaf.Value = rp.Value
	}
	return leaf, nil
}

// BuildPredicate compiles a validated predicate tree into one WHERE
// [Condition]: a fully-parenthesized SQL string with sequential $N
// placeholders in depth-first order and a matching, same-order Args
// slice. Field names are interpolated (they came from the schema
// allow-list in ParseWhere); every value is a bound arg. Hand the result
// to qb.Where(c.SQL, c.Args...) ONCE — the builder wraps it in its own
// parens and AND-joins it to the framework scopes, so the user's boolean
// logic can never escape to widen a scope.
//
// The $N numbers are positional only; core/query.renumberPlaceholders
// rewrites them left-to-right and advances by len(Args), so the sole
// invariant is that placeholders appear in the same order as Args — which
// building both in one DFS pass guarantees.
func BuildPredicate(p *Predicate) Condition {
	if p == nil {
		return Condition{}
	}
	var args []any
	sql := buildPredSQL(*p, &args)
	return Condition{SQL: sql, Args: args}
}

func buildPredSQL(p Predicate, args *[]any) string {
	if p.isGroup() {
		parts := make([]string, len(p.Children))
		for i, c := range p.Children {
			parts[i] = buildPredSQL(c, args)
		}
		conj := " AND "
		if p.Or {
			conj = " OR "
		}
		return "(" + strings.Join(parts, conj) + ")"
	}
	// Leaf — mirror ApplyToQuery's per-op SQL exactly.
	switch p.Op {
	case OpIn:
		ph := make([]string, len(p.Values))
		for i, v := range p.Values {
			*args = append(*args, v)
			ph[i] = "$" + itoa(len(*args))
		}
		return "(" + p.Field + " IN (" + strings.Join(ph, ",") + "))"
	case OpLike:
		*args = append(*args, escapeLikePattern(p.Value))
		return "(" + p.Field + ` LIKE $` + itoa(len(*args)) + ` ESCAPE '\')`
	default:
		*args = append(*args, p.Value)
		return "(" + p.Field + " " + sqlOp(p.Op) + " $" + itoa(len(*args)) + ")"
	}
}

// sqlOp maps a scalar FilterOp to its SQL comparison operator. IN and
// LIKE are handled inline in buildPredSQL; this covers the rest.
func sqlOp(op FilterOp) string {
	switch op {
	case OpGt:
		return ">"
	case OpLt:
		return "<"
	case OpGte:
		return ">="
	case OpLte:
		return "<="
	default: // OpEq
		return "="
	}
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}
