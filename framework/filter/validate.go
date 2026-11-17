package filter

import (
	"strconv"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

var knownPredicateOps = func() map[FilterOp]bool {
	m := make(map[FilterOp]bool, len(whereOps))
	for _, op := range whereOps {
		m[op] = true
	}
	return m
}()

var textualColumnTypes = map[schema.FieldType]bool{
	schema.String: true,
	schema.Text:   true,
	schema.Enum:   true,
	schema.UUID:   true,
}

// OpSuitsType reports whether op may filter a column of type t: no op
// on JSON, like only on text-shaped columns, no ordered comparison on
// Bool. Every filter surface and the OpenAPI spec read it.
func OpSuitsType(op FilterOp, t schema.FieldType) bool {
	if t == schema.JSON {
		return false
	}
	switch op {
	case OpLike:
		return textualColumnTypes[t]
	case OpGt, OpLt, OpGte, OpLte:
		return t != schema.Bool
	default:
		return true
	}
}

// CheckOpType refuses, naming field, an op that OpSuitsType refuses.
func CheckOpType(field string, op FilterOp, t schema.FieldType) error {
	if OpSuitsType(op, t) {
		return nil
	}
	return i18n.NewError(reasonOpTypeMismatch, opTypeParams(field, op, t))
}

func opTypeParams(field string, op FilterOp, t schema.FieldType) map[string]any {
	return map[string]any{"op": strconv.Quote(string(op)), "field": strconv.Quote(field), "type": fieldTypeName(t)}
}

func fieldTypeName(t schema.FieldType) string {
	switch t {
	case schema.String:
		return "string"
	case schema.Text:
		return "text"
	case schema.Int:
		return "int"
	case schema.Float:
		return "float"
	case schema.Decimal:
		return "decimal"
	case schema.Bool:
		return "bool"
	case schema.Enum:
		return "enum"
	case schema.UUID:
		return "uuid"
	case schema.Timestamp:
		return "timestamp"
	case schema.Date:
		return "date"
	case schema.JSON:
		return "json"
	case schema.Relation:
		return "relation"
	case schema.Image:
		return "image"
	case schema.File:
		return "file"
	}
	return "unknown"
}

func validateLeaf(p *Predicate, idx *fieldIndex) error {
	if p.Field == "" {
		return i18n.NewError(reasonWhereNoField)
	}
	if idx.noQuery[p.Field] {
		return i18n.NewError(reasonWhereNotFilterable, map[string]any{"field": strconv.Quote(p.Field)})
	}
	if !idx.allow[p.Field] {
		return i18n.NewError(reasonWhereUnknownField, map[string]any{"field": strconv.Quote(p.Field)})
	}
	sent := p.Field
	if col, ok := idx.alias[p.Field]; ok {
		p.Field = col
	}
	if !knownPredicateOps[p.Op] {
		return i18n.NewError(reasonWhereUnknownOp, map[string]any{"op": strconv.Quote(string(p.Op))})
	}
	if t, ok := idx.colType[p.Field]; ok {
		if !OpSuitsType(p.Op, t) {
			return i18n.NewError(reasonWhereOpTypeMismatch, opTypeParams(p.Field, p.Op, t))
		}
	}
	if p.Op == OpIn {
		if len(p.Values) == 0 {
			return i18n.NewError(reasonWhereInNeedsValues, map[string]any{"field": strconv.Quote(p.Field)})
		}
		if len(p.Values) > MaxINListEntries {
			return i18n.NewError(reasonWhereInOverCap, map[string]any{"max": MaxINListEntries})
		}
	}
	t := idx.colType[p.Field]
	switch p.Op {
	case OpLike:
		if !textsafe.Storable(p.Value) {
			return valueTypeError(sent, t)
		}
	case OpIn:
		for _, v := range p.Values {
			if _, err := CoerceValue(sent, t, v); err != nil {
				return err
			}
		}
	default:
		if _, err := CoerceValue(sent, t, p.Value); err != nil {
			return err
		}
	}
	p.colType = t
	return nil
}

// ValidatePredicate runs ParseWhere's checks over a tree built in Go and
// returns a validated deep copy to hand BuildPredicate; the input is
// never written. A nil tree returns (nil, nil).
func ValidatePredicate(p *Predicate, fields []schema.Field) (*Predicate, error) {
	if p == nil {
		return nil, nil
	}
	idx := newFieldIndex(fields)
	count := 0
	resolved := copyPredicateTree(p)
	if err := validatePredicateNode(resolved, idx, 1, &count); err != nil {
		return nil, err
	}
	return resolved, nil
}

func copyPredicateTree(p *Predicate) *Predicate {
	c := *p
	if len(p.Values) > 0 {
		c.Values = append([]string(nil), p.Values...)
	}
	c.Children = nil
	if len(p.Children) > 0 {
		c.Children = make([]Predicate, len(p.Children))
		for i := range p.Children {
			c.Children[i] = *copyPredicateTree(&p.Children[i])
		}
	}
	return &c
}

func validatePredicateNode(p *Predicate, idx *fieldIndex, depth int, count *int) error {
	if depth > maxPredicateDepth {
		return i18n.NewError(reasonWhereTooDeep, map[string]any{"max": maxPredicateDepth})
	}
	*count++
	if *count > maxPredicateNodes {
		return i18n.NewError(reasonWhereTooManyNodes, map[string]any{"max": maxPredicateNodes})
	}
	if len(p.Children) > 0 {
		if p.Field != "" {
			return i18n.NewError(reasonWhereNodeShape)
		}
		for i := range p.Children {
			if err := validatePredicateNode(&p.Children[i], idx, depth+1, count); err != nil {
				return err
			}
		}
		return nil
	}
	return validateLeaf(p, idx)
}
