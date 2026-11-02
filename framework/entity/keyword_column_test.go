package entity

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/query"
)

// A typed column named with a keyword writes it the way the migrator's
// DDL does; any other name stays bare.
func TestTypedColumnQuotesKeyword(t *testing.T) {
	for _, c := range [][2]string{
		{NewStringColumn("order").Eq("x").SQL(), `"order" = $1`},
		{NewIntColumn("limit").Gte(1).SQL(), `"limit" >= $1`},
		{NewStringColumn("user").In("a").SQL(), `"user" IN ($1)`},
		{NewStringColumn("desc").IsNull().SQL(), `"desc" IS NULL`},
		{NewStringColumn("title").Eq("x").SQL(), `title = $1`},
		{NewStringColumn("title").IsNull().SQL(), `title IS NULL`},
		{NewBoolColumn("group").Eq(true).SQL(), `"group" = $1`},
		{NewStringColumn("Status").Like("x").SQL(), `Status LIKE $1`},
	} {
		if c[0] != c[1] {
			t.Errorf("got %s, want %s", c[0], c[1])
		}
	}
	qb := query.Select("*").From("posts")
	NewIntColumn("order").Desc().Apply(qb)
	if sql, _ := qb.Build(); sql != `SELECT * FROM posts ORDER BY "order" DESC` {
		t.Errorf("Desc on order = %s", sql)
	}
}
