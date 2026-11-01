package filter

import (
	"strconv"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/schema"
)

// predFieldIndex is the resolved allow-list a predicate's leaf fields are
// checked against: which names may be filtered, which are barred by NoQuery,
// how wire aliases resolve to columns, and each column's type. ParseWhere
// builds one per request from the URL tree; ValidatePredicate builds one for
// a tree that arrived as Go values. One shape, one set of rules, so the two
// entry points cannot drift.
type predFieldIndex struct {
	allow   map[string]bool
	noQuery map[string]bool
	alias   map[string]string
	colType map[string]schema.FieldType
}

// newPredicateFieldIndex projects an entity's fields onto the index. Hidden
// fields are skipped before either name is registered, so a hidden column
// stays unreachable under its column name AND its alias (the
// value-disclosure-oracle rationale documented on ParseFilters). NoQuery
// fields are registered under both spellings so the name clients are told to
// send is refused, not resolved past the guard.
func newPredicateFieldIndex(fields []schema.Field) *predFieldIndex {
	idx := &predFieldIndex{
		allow:   make(map[string]bool, len(fields)),
		alias:   make(map[string]string, len(fields)),
		colType: make(map[string]schema.FieldType, len(fields)),
	}
	for _, f := range fields {
		if f.Hidden {
			continue
		}
		if f.NoQuery {
			if idx.noQuery == nil {
				idx.noQuery = make(map[string]bool, len(fields))
			}
			idx.noQuery[f.Name] = true
			if f.WireName != "" && f.WireName != f.Name {
				idx.noQuery[f.WireName] = true
			}
			continue
		}
		idx.allow[f.Name] = true
		idx.colType[f.Name] = f.Type
		if f.WireName != "" && f.WireName != f.Name {
			idx.allow[f.WireName] = true
			idx.alias[f.WireName] = f.Name
		}
	}
	return idx
}

// knownPredicateOps is the set of operators a predicate leaf may carry,
// derived from whereOps so the token table stays the one source of truth.
var knownPredicateOps = func() map[FilterOp]bool {
	m := make(map[FilterOp]bool, len(whereOps))
	for _, op := range whereOps {
		m[op] = true
	}
	return m
}()

// textualColumnTypes are the column shapes OpLike is meaningful on.
var textualColumnTypes = map[schema.FieldType]bool{
	schema.String: true,
	schema.Text:   true,
	schema.Enum:   true,
	schema.UUID:   true,
}

// OpSuitsType reports whether op is meaningful on a column of type t. It
// is the one type predicate behind every filter surface: CheckOpType,
// the URL parsers, and the OpenAPI spec generator's advertised-operator
// set all read it, so no surface can accept (or advertise) an operator
// another refuses.
//
// OpLike is a text-shape question and is refused on non-text columns (a
// `like` on an Int or Bool is always a client bug, and on a JSON column
// it quietly depends on dialect-specific text coercion). The ordered
// comparisons are refused on Bool: SQL orders false < true, but no
// caller means that, and accepting it invites accidental always-true
// predicates. A JSON column takes no operator at all: equality compared
// the stored text on SQLite and the parsed value on Postgres (JSONB),
// so {"x":1.0} matched a stored {"x":1} on one engine and not the other,
// and neither engine can put the other's answer into SQL. OpEq, OpNe
// and OpIn suit every other type; their values are bound parameters.
func OpSuitsType(op FilterOp, t schema.FieldType) bool {
	if t == schema.JSON {
		return false
	}
	switch op {
	case OpLike:
		return textualColumnTypes[t]
	case OpGt, OpLt, OpGte, OpLte:
		// A Bool has two values and a JSON blob no order a request value
		// can compare against (Postgres casts the value to JSONB first).
		return t != schema.Bool && t != schema.JSON
	default: // OpEq, OpNe, OpIn
		return true
	}
}

// CheckOpType refuses an operator that does not suit the column's type,
// the one rule every filter surface applies: ?field_<op>=, ?where= and
// Go-built predicates, nested ?rel.field_<op>= filters and include-scoped
// filters. The error names the field the caller sent.
func CheckOpType(field string, op FilterOp, t schema.FieldType) error {
	if OpSuitsType(op, t) {
		return nil
	}
	return i18n.NewError(reasonOpTypeMismatch, opTypeParams(field, op, t))
}

func opTypeParams(field string, op FilterOp, t schema.FieldType) map[string]any {
	return map[string]any{"op": strconv.Quote(string(op)), "field": strconv.Quote(field), "type": fieldTypeName(t)}
}

// fieldTypeName renders t for an error message. schema.FieldType has no
// String method; this local spelling keeps refusal messages readable
// without widening core/schema's API.
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

// validateLeaf checks one leaf node against idx. It writes the two
// resolutions the tree that reaches SQL needs, on the COPY
// ValidatePredicate built (parseNode does the same on its fresh nodes
// at parse time): a wire alias resolves to the column name
// (Predicate.Field reaches the WHERE clause), and colType is (re)set
// from the schema so BuildPredicate binds typed values. Every value must
// be of the column's type (CoerceValue), the same check the flat
// ?field_<op>= parser runs. A caller that hand-set colType loses it here,
// the schema is the authority.
func validateLeaf(p *Predicate, idx *predFieldIndex) error {
	if p.Field == "" {
		return i18n.NewError(reasonWhereNoField)
	}
	if idx.noQuery[p.Field] {
		// Visible in responses but barred from the query surface, so the
		// field can be named without disclosing anything new.
		return i18n.NewError(reasonWhereNotFilterable, map[string]any{"field": strconv.Quote(p.Field)})
	}
	if !idx.allow[p.Field] {
		// Unknown or Hidden field, never build a predicate on it. Folding
		// Hidden into unknown keeps a hidden column indistinguishable from
		// an absent one.
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
		// A substring, matched as text; never coerced, but bound as
		// text, so it must be text Postgres stores (CoerceValue).
		if !storableText(p.Value) {
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

// ValidatePredicate runs over a predicate tree built in Go code — a view
// func's return value, a ListOptions.Where, anything that did not arrive
// through ParseWhere — the SAME checks ParseWhere applies to input from a
// URL: every leaf field exists on the schema and is neither Hidden nor
// NoQuery (Hidden under either its column name or its wire alias), the
// operator is known and suits the field's type, an OpIn leaf carries 1…
// MaxINListEntries values, and the tree stays inside the depth and node
// caps.
//
// It never writes to the input: the caller's tree may be shared across
// goroutines (a parsed Display view handed to concurrent requests), so
// the two resolutions a validated tree carries — a wire alias resolved
// to its column name, a Bool leaf's coercion marker — are written on a
// deep copy that is returned, and that copy is the tree to hand
// BuildPredicate. The input stays byte-for-byte what the caller built.
//
// Field names in a predicate are spliced into SQL by BuildPredicate (values
// are bound as placeholders, names are not), so this check is what keeps a
// Go-built tree safe to compile: a field that is not on the schema's
// allow-list — an invented name, a Hidden column, SQL metacharacters in a
// key — is refused before any SQL exists. A node that is neither a
// well-formed leaf nor a non-empty group is refused too.
//
// nil is valid (the empty predicate) and returns (nil, nil).
func ValidatePredicate(p *Predicate, fields []schema.Field) (*Predicate, error) {
	if p == nil {
		return nil, nil
	}
	idx := newPredicateFieldIndex(fields)
	count := 0
	resolved := copyPredicateTree(p)
	if err := validatePredicateNode(resolved, idx, 1, &count); err != nil {
		return nil, err
	}
	return resolved, nil
}

// copyPredicateTree deep-copies a predicate tree: every node and every
// Values slice is rebuilt, so the resolutions validateLeaf writes land on
// memory the caller of ValidatePredicate owns alone.
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

func validatePredicateNode(p *Predicate, idx *predFieldIndex, depth int, count *int) error {
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
