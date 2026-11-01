package filter

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/schema"
)

// Predicate is a node in a boolean filter tree: either a LEAF (one
// field/op/value comparison) or a GROUP (AND/OR of child predicates).
// It is the parsed, validated form of a `?where=<json>` request, every
// field has already been checked against the entity's schema allow-list
// and every operator against the supported set, so [BuildPredicate] may
// interpolate field names trusting they are safe while binding all
// values as placeholders.
//
// Groups let callers express nested boolean logic the flat query-param
// filters cannot, e.g. `status = A OR (priority = high AND assignee =
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
	// colType is the leaf column's schema type, set by validation (ParseWhere,
	// ValidatePredicate) after every value passed CoerceValue. BuildPredicate
	// binds TypedBind(colType, v): a Go bool, int64 or float64 where the
	// column wants one, same rationale as ParsedFilter's typed value. The
	// zero value (String) binds the raw text, so an unvalidated tree binds
	// as it always did.
	colType schema.FieldType

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
	"ne":   OpNe,
	"gt":   OpGt,
	"lt":   OpLt,
	"gte":  OpGte,
	"lte":  OpLte,
	"like": OpLike,
	"in":   OpIn,
}

// rawPred is the wire shape of one `?where=` node. Exactly one of
// {and, or, field} is expected per node. The members are pointers so a
// member that was sent can be told from one that was not: a leaf may
// carry only the value member its op reads, and a group no leaf member.
type rawPred struct {
	And    *[]json.RawMessage `json:"and"`
	Or     *[]json.RawMessage `json:"or"`
	Field  *string            `json:"field"`
	Op     *string            `json:"op"`
	Value  *string            `json:"value"`
	Values *[]string          `json:"values"`
}

// ParseWhere parses a `?where=<json>` predicate tree and validates every
// leaf field against the schema allow-list (Hidden fields excluded, same
// value-disclosure-oracle rationale as ParseFilters) and every operator
// against the supported set. Returns (nil, nil) when raw is empty. Any
// unknown field, unknown operator, malformed JSON, empty/ambiguous node,
// or a tree that exceeds the depth/node bounds returns an error, the
// caller maps it to 400. On success the tree is safe for BuildPredicate
// to compile.
func ParseWhere(raw string, fields []schema.Field) (*Predicate, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	// Accept a field's wire key as well as its column name, matching
	// ParseFiltersValues, ParseSortValues and ?fields= projection; see
	// newPredicateFieldIndex for the Hidden/NoQuery/alias rules, which
	// ValidatePredicate shares so the URL surface and the Go surface
	// cannot drift.
	idx := newPredicateFieldIndex(fields)
	var msg json.RawMessage
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		return nil, &i18n.Error{Reason: reasonWhereInvalidJSON, Params: map[string]any{"detail": handler.JSONErrorDetail(err)}, Err: err}
	}
	count := 0
	p, err := parseNode(msg, idx, 1, &count)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func parseNode(msg json.RawMessage, idx *predFieldIndex, depth int, count *int) (Predicate, error) {
	if depth > maxPredicateDepth {
		return Predicate{}, i18n.NewError(reasonWhereTooDeep, map[string]any{"max": maxPredicateDepth})
	}
	*count++
	if *count > maxPredicateNodes {
		return Predicate{}, i18n.NewError(reasonWhereTooManyNodes, map[string]any{"max": maxPredicateNodes})
	}
	// The tree is client-borne JSON, so each node decodes under the rule
	// every other client-borne body does (handler.UnmarshalStrict; the
	// cursor token is checked the same way): stdlib json resolves a
	// duplicate or case-folded key last-wins and drops an unknown one, so
	// {"field":"status","value":"a","VALUE":"b"} filtered on b while a
	// proxy reading the first key saw a, and a typo'd "vaule" compared
	// status with the empty string. UnmarshalStrict refuses a repeated or
	// folded key at every depth below the node too, and unknown or
	// case-folded member names on the node itself.
	var rp rawPred
	if err := handler.UnmarshalStrict(msg, &rp); err != nil {
		return Predicate{}, &i18n.Error{Reason: reasonWhereInvalidNode, Params: map[string]any{"detail": handler.JSONErrorDetail(err)}, Err: err}
	}

	// A member sent empty counts as sent: {"and":[],"field":"x"} is not
	// a leaf with a stray member ignored, it is two shapes at once.
	hasAnd, hasOr, hasField := rp.And != nil, rp.Or != nil, rp.Field != nil && *rp.Field != ""
	// Exactly one kind per node, and a group carries no leaf member.
	if btoi(hasAnd)+btoi(hasOr)+btoi(hasField) != 1 {
		return Predicate{}, i18n.NewError(reasonWhereNodeShape)
	}

	if hasAnd || hasOr {
		if rp.Field != nil || rp.Op != nil || rp.Value != nil || rp.Values != nil {
			return Predicate{}, i18n.NewError(reasonWhereNodeShape)
		}
		var kids []json.RawMessage
		or := false
		if hasOr {
			kids, or = *rp.Or, true
		} else {
			kids = *rp.And
		}
		if len(kids) == 0 {
			return Predicate{}, i18n.NewError(reasonWhereNodeShape)
		}
		children := make([]Predicate, 0, len(kids))
		for _, k := range kids {
			c, err := parseNode(k, idx, depth+1, count)
			if err != nil {
				return Predicate{}, err
			}
			children = append(children, c)
		}
		return Predicate{Or: or, Children: children}, nil
	}

	// Leaf. The field/operator/type checks live in validateLeaf, shared
	// with ValidatePredicate so the URL surface and the Go-built surface
	// answer identically.
	opTok := ""
	if rp.Op != nil {
		opTok = *rp.Op
	}
	if opTok == "" {
		opTok = "eq"
	}
	op, ok := whereOps[opTok]
	if !ok {
		return Predicate{}, i18n.NewError(reasonWhereUnknownOp, map[string]any{"op": strconv.Quote(opTok)})
	}
	leaf := Predicate{Field: *rp.Field, Op: op}
	if op == OpIn {
		// in reads "values" or a comma string in "value", never both:
		// with both sent, one was silently dropped.
		if rp.Value != nil && rp.Values != nil {
			return Predicate{}, i18n.NewError(reasonWhereStrayMember, map[string]any{"op": strconv.Quote(opTok), "member": strconv.Quote("value")})
		}
		var vals []string
		if rp.Values != nil {
			vals = *rp.Values
		} else if rp.Value != nil && *rp.Value != "" {
			// Count separators before splitting. The where-body is size-bounded,
			// so this is defence in depth rather than a live DoS, but the
			// allocate-then-reject shape is the same one SplitINValuesBounded
			// exists to avoid on the ?field_in= path, keep them consistent.
			if strings.Count(*rp.Value, ",")+1 > MaxINListEntries {
				return Predicate{}, i18n.NewError(reasonWhereInOverCap, map[string]any{"max": MaxINListEntries})
			}
			vals = strings.Split(*rp.Value, ",")
		}
		leaf.Values = vals
	} else {
		// A scalar op reads "value" alone; "values" sent beside it was
		// dropped, and the leaf compared against ''.
		if rp.Values != nil {
			return Predicate{}, i18n.NewError(reasonWhereStrayMember, map[string]any{"op": strconv.Quote(opTok), "member": strconv.Quote("values")})
		}
		if rp.Value != nil {
			leaf.Value = *rp.Value
		}
	}
	if err := validateLeaf(&leaf, idx); err != nil {
		return Predicate{}, err
	}
	return leaf, nil
}

// BuildPredicate compiles a validated predicate tree into one WHERE
// [Condition]: a fully-parenthesized SQL string with sequential $N
// placeholders in depth-first order and a matching, same-order Args
// slice. Field names are interpolated (they came from the schema
// allow-list in ParseWhere); every value is a bound arg. Hand the result
// to qb.Where(c.SQL, c.Args...) ONCE, the builder wraps it in its own
// parens and AND-joins it to the framework scopes, so the user's boolean
// logic can never escape to widen a scope.
//
// The $N numbers are positional only; core/query.renumberPlaceholders
// rewrites them left-to-right and advances by len(Args), so the sole
// invariant is that placeholders appear in the same order as Args, which
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
	// Leaf, mirror ApplyToQuery's per-op SQL exactly.
	switch p.Op {
	case OpIn:
		ph := make([]string, len(p.Values))
		for i, v := range p.Values {
			*args = append(*args, TypedBind(p.colType, v))
			ph[i] = "$" + strconv.Itoa(len(*args))
		}
		return "(" + p.Field + " IN (" + strings.Join(ph, ",") + "))"
	case OpLike:
		clause, arg := LikeClause(p.Field, len(*args)+1, p.Value)
		*args = append(*args, arg)
		return "(" + clause + ")"
	default:
		*args = append(*args, TypedBind(p.colType, p.Value))
		return "(" + p.Field + " " + sqlOp(p.Op) + " $" + strconv.Itoa(len(*args)) + ")"
	}
}

// sqlOp maps a scalar FilterOp to its SQL comparison operator. IN and
// LIKE are handled inline in buildPredSQL; this covers the rest.
func sqlOp(op FilterOp) string {
	switch op {
	case OpNe:
		return "!="
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
