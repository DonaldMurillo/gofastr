package dsl

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// TestDSLOperatorsHonourOpSuitsType pins every where() operator to
// filter.OpSuitsType, "the one type predicate behind every filter
// surface". BuildDSLQuery consulted it only for contains, so an ordered
// compare on a Bool and any operator on a JSON column, both refused by
// ?field_<op>= and ?where=, ran here.
func TestDSLOperatorsHonourOpSuitsType(t *testing.T) {
	reg := postsRegistry(t, func(c *entity.EntityConfig) {
		c.Fields = append(c.Fields,
			schema.Field{Name: "active", Type: schema.Bool},
			schema.Field{Name: "meta", Type: schema.JSON},
		)
	})
	for _, q := range []string{
		`posts.where(active > true)`,
		`posts.where(active <= false)`,
		`posts.where(meta = "{}")`,
		`posts.where(meta != "{}")`,
		`posts.where(meta in ["{}"])`,
		`posts.where(seq contains "1")`,
	} {
		if _, err := BuildDSLQuery(reg, q); err == nil {
			t.Errorf("%s: accepted; every other filter surface refuses this operator/type pair", q)
		}
	}
	for _, q := range []string{
		`posts.where(active = true)`,
		`posts.where(active != false)`,
		`posts.where(seq >= 3, status contains "pub")`,
		`posts.where(active in [true, false])`,
	} {
		if _, err := BuildDSLQuery(reg, q); err != nil {
			t.Errorf("%s: %v", q, err)
		}
	}
	// contains is a text bind: bytes Postgres cannot store are refused.
	if _, err := BuildDSLQuery(reg, "posts.where(status contains \"a\x00b\")"); err == nil {
		t.Errorf("contains with a NUL accepted")
	}
}
