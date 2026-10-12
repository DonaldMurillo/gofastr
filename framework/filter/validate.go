package filter

import (
	"fmt"

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
	boolCol map[string]bool
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
		boolCol: make(map[string]bool, len(fields)),
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
		if f.Type == schema.Bool {
			idx.boolCol[f.Name] = true
		}
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
// predicates. OpEq, OpNe and OpIn suit every type; their values are
// bound parameters.
func OpSuitsType(op FilterOp, t schema.FieldType) bool {
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
	return fmt.Errorf("operator %q cannot filter %q (type %s)", op, field, fieldTypeName(t))
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
// (Predicate.Field reaches the WHERE clause), and isBool is (re)set
// from the schema so BuildPredicate coerces true/false spellings to Go
// bools. A caller that hand-set isBool on a non-Bool column loses it
// here, the schema is the authority.
func validateLeaf(p *Predicate, idx *predFieldIndex) error {
	if p.Field == "" {
		return fmt.Errorf("where: predicate leaf has no field")
	}
	if idx.noQuery[p.Field] {
		// Visible in responses but barred from the query surface, so the
		// field can be named without disclosing anything new.
		return fmt.Errorf("where: field %q cannot be filtered", p.Field)
	}
	if !idx.allow[p.Field] {
		// Unknown or Hidden field, never build a predicate on it. Folding
		// Hidden into unknown keeps a hidden column indistinguishable from
		// an absent one.
		return fmt.Errorf("where: unknown filter field %q", p.Field)
	}
	if col, ok := idx.alias[p.Field]; ok {
		p.Field = col
	}
	if !knownPredicateOps[p.Op] {
		return fmt.Errorf("where: unknown operator %q", p.Op)
	}
	if t, ok := idx.colType[p.Field]; ok {
		if err := CheckOpType(p.Field, p.Op, t); err != nil {
			return fmt.Errorf("where: %w", err)
		}
	}
	if p.Op == OpIn {
		if len(p.Values) == 0 {
			return fmt.Errorf("where: %q with op in requires values", p.Field)
		}
		if len(p.Values) > MaxINListEntries {
			return fmt.Errorf("where: in-list exceeds %d entries", MaxINListEntries)
		}
	}
	p.isBool = idx.boolCol[p.Field]
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
		return fmt.Errorf("where: nesting exceeds max depth %d", maxPredicateDepth)
	}
	*count++
	if *count > maxPredicateNodes {
		return fmt.Errorf("where: exceeds max node count %d", maxPredicateNodes)
	}
	if len(p.Children) > 0 {
		if p.Field != "" {
			return fmt.Errorf("where: each node must be exactly one of and/or/field")
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
