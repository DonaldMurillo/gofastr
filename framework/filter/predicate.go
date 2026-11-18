package filter

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
)

// Predicate is a node in a boolean filter tree: a leaf (one field, op and
// value) or a group (AND or OR of Children), as ParseWhere or
// ValidatePredicate return it for BuildPredicate.
type Predicate struct {
	Field  string
	Op     FilterOp
	Value  string   // for scalar ops
	Values []string // for OpIn
	// Or joins Children with OR; false joins them with AND.
	Or       bool
	Children []Predicate
	colType  schema.FieldType
}

func (p *Predicate) isGroup() bool { return len(p.Children) > 0 }

const (
	maxPredicateDepth = 8
	maxPredicateNodes = 64
)

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

type rawPred struct {
	And    *[]json.RawMessage `json:"and"`
	Or     *[]json.RawMessage `json:"or"`
	Field  *string            `json:"field"`
	Op     *string            `json:"op"`
	Value  *string            `json:"value"`
	Values *[]*string         `json:"values"`
}

// ParseWhere parses and validates a ?where=<json> tree against fields.
// It returns (nil, nil) for an empty raw, and errors on malformed JSON, a
// node that is not exactly one of and, or and field, an unknown, Hidden
// or NoQuery field, an unknown or unsuited op, a mistyped value, or a
// tree past the depth or node bounds.
func ParseWhere(raw string, fields []schema.Field) (*Predicate, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	idx := newFieldIndex(fields)
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

func parseNode(msg json.RawMessage, idx *fieldIndex, depth int, count *int) (Predicate, error) {
	if depth > maxPredicateDepth {
		return Predicate{}, i18n.NewError(reasonWhereTooDeep, map[string]any{"max": maxPredicateDepth})
	}
	*count++
	if *count > maxPredicateNodes {
		return Predicate{}, i18n.NewError(reasonWhereTooManyNodes, map[string]any{"max": maxPredicateNodes})
	}
	var rp rawPred
	if err := handler.UnmarshalStrict(msg, &rp); err != nil {
		return Predicate{}, &i18n.Error{Reason: reasonWhereInvalidNode, Params: map[string]any{"detail": handler.JSONErrorDetail(err)}, Err: err}
	}
	hasAnd, hasOr, hasField := rp.And != nil, rp.Or != nil, rp.Field != nil && *rp.Field != ""
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
		if rp.Value != nil && rp.Values != nil {
			return Predicate{}, i18n.NewError(reasonWhereStrayMember, map[string]any{"op": strconv.Quote(opTok), "member": strconv.Quote("value")})
		}
		var vals []string
		if rp.Values != nil {
			vals = make([]string, 0, len(*rp.Values))
			for _, v := range *rp.Values {
				if v == nil {
					return Predicate{}, i18n.NewError(reasonWhereNeedsValue, map[string]any{"field": strconv.Quote(*rp.Field), "op": strconv.Quote(opTok)})
				}
				vals = append(vals, *v)
			}
		} else if rp.Value != nil && *rp.Value != "" {
			if strings.Count(*rp.Value, ",")+1 > MaxINListEntries {
				return Predicate{}, i18n.NewError(reasonWhereInOverCap, map[string]any{"max": MaxINListEntries})
			}
			vals = strings.Split(*rp.Value, ",")
		}
		leaf.Values = vals
	} else {
		if rp.Values != nil {
			return Predicate{}, i18n.NewError(reasonWhereStrayMember, map[string]any{"op": strconv.Quote(opTok), "member": strconv.Quote("values")})
		}
		if rp.Value == nil {
			return Predicate{}, i18n.NewError(reasonWhereNeedsValue, map[string]any{"field": strconv.Quote(*rp.Field), "op": strconv.Quote(opTok)})
		}
		leaf.Value = *rp.Value
	}
	if err := validateLeaf(&leaf, idx); err != nil {
		return Predicate{}, err
	}
	return leaf, nil
}

// BuildPredicate compiles a validated tree into one parenthesized WHERE
// Condition whose $N placeholders follow Args in order. Pass it to
// qb.Where once.
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
	switch p.Op {
	case OpIn:
		clause := inClause(p.Field, len(p.Values), len(*args))
		for _, v := range p.Values {
			*args = append(*args, TypedBind(p.colType, v))
		}
		return "(" + clause + ")"
	case OpLike:
		clause, arg := LikeClause(p.Field, len(*args)+1, p.Value)
		*args = append(*args, arg)
		return "(" + clause + ")"
	default:
		*args = append(*args, TypedBind(p.colType, p.Value))
		return "(" + query.QuoteKeyword(p.Field) + " " + sqlOp(p.Op) + " $" + strconv.Itoa(len(*args)) + ")"
	}
}

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
	default:
		return "="
	}
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}
