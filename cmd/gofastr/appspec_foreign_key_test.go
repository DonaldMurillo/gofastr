package main

import (
	"go/ast"
	"go/parser"
	"slices"
	"strings"
	"testing"
)

// orderLinesYAML: lines belong to an order keyed on (store, no) through a
// two-column foreign key.
const orderLinesYAML = `  - name: orders
    primary_key: [store, no]
    fields:
      - name: store
        type: string
        required: true
      - name: no
        type: int
        required: true
  - name: lines
    fields:
      - name: order_store
        type: string
      - name: order_no
        type: int
      - name: sku
        type: string
    relations:
      - type: belongs_to
        name: order
        entity: orders
        foreign_key: [order_store, order_no]
`

// A foreign_key list reaches Relation.ForeignKey, the generated
// registration, pack, and the YAML round trip, in declared order.
func TestForeignKeyListReachesRegistration(t *testing.T) {
	bp, err := loadBPWithEntity(t, orderLinesYAML)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	want := []string{"order_store", "order_no"}
	decl := bp.Entities[1]
	if rel := decl.Relations[0]; !slices.Equal(rel.ForeignKey, want) {
		t.Fatalf("ForeignKey = %v, want %v", rel.ForeignKey, want)
	}
	src, err := renderEntityRegistration(decl)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(src, `ForeignKey: []string{"order_store", "order_no"}`) {
		t.Fatalf("registration lacks the foreign key:\n%s", src)
	}
	call, err := parser.ParseExpr(strings.TrimSpace(src[:strings.Index(src, "\n\t_ = ")]))
	if err != nil {
		t.Fatalf("parse registration: %v\n%s", err, src)
	}
	packed, err := packEntityDeclFromCall(call.(*ast.CallExpr))
	if err != nil || len(packed.Relations) != 1 || !slices.Equal(packed.Relations[0].ForeignKey, want) {
		t.Fatalf("packed relations = %+v, %v; want ForeignKey %v", packed.Relations, err, want)
	}

	out, err := encodeAppSpecYAML(bp)
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	again, err := decodeAppSpecString(out)
	if err != nil {
		t.Fatalf("re-parse: %v\n%s", err, out)
	}
	if got := again.Entities[1].Relations[0].ForeignKey; !slices.Equal(got, want) {
		t.Errorf("YAML round trip ForeignKey = %v; want %v\n%s", got, want, out)
	}
}

// A one-element list is the one-column spelling.
func TestForeignKeyListOfOne(t *testing.T) {
	bp, err := loadBPWithEntity(t, `  - name: authors
    fields:
      - name: name
        type: string
  - name: posts
    fields:
      - name: author_id
        type: string
    relations:
      - type: belongs_to
        name: author
        entity: authors
        foreign_key: [author_id]
`)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if rel := bp.Entities[1].Relations[0]; !slices.Equal(rel.ForeignKey, []string{"author_id"}) {
		t.Fatalf("ForeignKey = %v, want [author_id]", rel.ForeignKey)
	}
}

// A foreign key that does not line up with the key it references is
// refused when the spec loads, naming the problem, not when the
// generated app boots.
func TestForeignKeyRefusedAtLoad(t *testing.T) {
	for name, tc := range map[string]struct{ from, to, want string }{
		"empty list": {"foreign_key: [order_store, order_no]", "foreign_key: []", "non-empty list"},
		"one column": {"foreign_key: [order_store, order_no]", "foreign_key: order_no", "one foreign-key column per key column"},
		"three":      {"foreign_key: [order_store, order_no]", "foreign_key: [order_store, order_no, sku]", "one foreign-key column per key column"},
		"repeated":   {"foreign_key: [order_store, order_no]", "foreign_key: [order_no, ORDER_NO]", "twice"},
		"undeclared": {"foreign_key: [order_store, order_no]", "foreign_key: [order_store, order_ref]", `"order_ref", which is not a declared field`},
		"mistyped":   {"name: order_no\n        type: int", "name: order_no\n        type: string", "type each column like the key column"},
		"uuid":       {"name: order_no\n        type: int", "name: order_no\n        type: uuid", "type each column like the key column"},
		"float":      {"name: order_no\n        type: int", "name: order_no\n        type: float", "type each column like the key column"},
		"int text":   {"name: order_store\n        type: string", "name: order_store\n        type: int", "type each column like the key column"},
		"many to many": {"type: belongs_to\n        name: order\n        entity: orders\n        foreign_key: [order_store, order_no]",
			"type: many_to_many\n        name: order\n        entity: orders\n        through: line_orders\n        local_key: line_id\n        foreign_key_target: order_id", "join entity"},
	} {
		t.Run(name, func(t *testing.T) {
			spec := strings.Replace(orderLinesYAML, tc.from, tc.to, 1)
			if spec == orderLinesYAML {
				t.Fatalf("the edit %q did not apply", tc.from)
			}
			_, err := loadBPWithEntity(t, spec)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("load err = %v; want it to name %q", err, tc.want)
			}
		})
	}
}

// Every text-backed type joins a string key column, as the migrator
// reads them all as TEXT.
func TestForeignKeyTextTypesLoad(t *testing.T) {
	for _, typ := range []string{"text", "uuid", "enum\n        values: [a, b]", "string\n        max: 20"} {
		spec := strings.Replace(orderLinesYAML, "name: order_store\n        type: string", "name: order_store\n        type: "+typ, 1)
		if _, err := loadBPWithEntity(t, spec); err != nil {
			t.Fatalf("order_store typed %s: %v", typ, err)
		}
	}
}

// A column a transition writes stays out of a multi-column foreign key:
// the move would change the reference with no check.
func TestStateFieldRefusedInForeignKey(t *testing.T) {
	spec := strings.Replace(orderLinesYAML, "      - name: sku\n        type: string\n",
		"      - name: sku\n        type: string\n      - name: phase\n        type: enum\n        values: [a, b]\n        default: a\n", 1)
	spec += "    states:\n      field: phase\n      transitions:\n        - key: go\n          from: [a]\n          to: b\n          stamp: STAMP\n"
	if _, err := loadBPWithEntity(t, strings.Replace(spec, "STAMP", "sku", 1)); err != nil {
		t.Fatalf("states outside the key should load: %v", err)
	}
	for name, s := range map[string]string{
		"state field": strings.Replace(strings.Replace(spec, "field: phase", "field: order_store", 1), "STAMP", "sku", 1),
		"stamp":       strings.Replace(spec, "STAMP", "order_no", 1),
	} {
		if _, err := loadBPWithEntity(t, s); err == nil || !strings.Contains(err.Error(), "transitions write") {
			t.Errorf("%s: load err = %v; want the column refused", name, err)
		}
	}
}

// The has_many end checks the same shape from the other side: the
// columns live on the child, the key on the declaring parent.
func TestHasManyForeignKeyChecked(t *testing.T) {
	spec := func(fk string) string {
		return `  - name: orders
    primary_key: [store, no]
    fields:
      - name: store
        type: string
        required: true
      - name: no
        type: int
        required: true
    relations:
      - type: has_many
        name: lines
        entity: lines
        foreign_key: ` + fk + `
  - name: lines
    fields:
      - name: order_store
        type: string
      - name: order_no
        type: int
`
	}
	_, err := loadBPWithEntity(t, spec("order_no"))
	if err == nil || !strings.Contains(err.Error(), `"orders" is keyed on (store, no)`) {
		t.Fatalf("load err = %v; want the has_many refused against the parent key", err)
	}
	if _, err := loadBPWithEntity(t, spec("[order_store, order_no]")); err != nil {
		t.Fatalf("a two-column has_many should load: %v", err)
	}
}
