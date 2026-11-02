package query

import "testing"

func TestQuoteKeyword(t *testing.T) {
	for in, want := range map[string]string{
		"title":          "title",
		"order":          `"order"`,
		"ORDER":          `"order"`,
		"Group":          `"group"`,
		"user":           "\"user\"",
		"posts.order":    `posts."order"`,
		"order.posts":    `"order".posts`,
		"main.user.desc": `main."user"."desc"`,
		"order_id":       "order_id",
		"key":            "key",
		"status":         "status",
		`"order"`:        `"order"`,
		"*":              "*",
	} {
		if got := QuoteKeyword(in); got != want {
			t.Errorf("QuoteKeyword(%q) = %s, want %s", in, got, want)
		}
	}
	if got := QuoteKeyword(QuoteKeyword("posts.limit")); got != `posts."limit"` {
		t.Errorf("QuoteKeyword is not idempotent: %s", got)
	}
}

// Every slot a builder fills with a name quotes a keyword the way the
// migrator's DDL does (QuoteFolded), so a column named order, group or user
// is the column the DDL created.
func TestBuildersQuoteKeywordNames(t *testing.T) {
	cases := []struct {
		name string
		sql  string
		want string
	}{
		{"insert", first(Insert("order").Columns("order", "group", "title").Values(1, "g", "t").Returning("id", "user").Build()),
			`INSERT INTO "order" ("order", "group", title) VALUES ($1, $2, $3) RETURNING id, "user"`},
		{"select", first(Select("id", "desc", "posts.limit").From("group").Order("order", "DESC").Order("title", "").Limit(5).Build()),
			`SELECT id, "desc", posts."limit" FROM "group" ORDER BY "order" DESC, title LIMIT $1`},
		{"join", first(Select("*").From("posts").Join("user", QuoteKeyword("user.id")+" = posts.author_id").Build()),
			`SELECT * FROM posts JOIN "user" ON "user".id = posts.author_id`},
		{"cursor", first(Select("*").From("posts").Cursor("end", 3, "forward").Build()),
			`SELECT * FROM posts WHERE ("end" > $1) ORDER BY "end" ASC`},
		{"cursor after order", first(Select("*").From("posts").Order("end", "ASC").Cursor("end", 3, "forward").Build()),
			`SELECT * FROM posts WHERE ("end" > $1) ORDER BY "end" ASC`},
		{"update", first(Update("order").Set("group", "g").Set("title", "t").Where("id = $1", 1).Returning("limit").Build()),
			`UPDATE "order" SET "group" = $1, title = $2 WHERE (id = $3) RETURNING "limit"`},
		{"update matching", first(Update("order").Where("id = $1", 1).Matching("user", "id")),
			`SELECT "user", id FROM "order" WHERE (id = $1)`},
		{"delete", first(Delete("table").Where("id = $1", 1).Returning("default").Build()),
			`DELETE FROM "table" WHERE (id = $1) RETURNING "default"`},
		{"count", first(Count("select").Build()), `SELECT COUNT(*) FROM "select"`},
	}
	for _, c := range cases {
		if c.sql != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.name, c.sql, c.want)
		}
	}
}

// A query that names no keyword is byte-identical to what the builders
// wrote before keyword quoting: the golden below is that SQL.
func TestBuildersLeaveOtherNamesBare(t *testing.T) {
	cases := []struct {
		sql, want string
	}{
		{first(Insert("posts").Columns("title", "author_id", "Status").Values(1, 2, 3).Returning("id", "created_at").Build()),
			`INSERT INTO posts (title, author_id, Status) VALUES ($1, $2, $3) RETURNING id, created_at`},
		{first(Select("id", "title", "posts.author_id").From("app.posts").LeftJoin("authors", "authors.id = posts.author_id").
			Where("title = $1", "x").OrWhere("status IN ($1, $2)", "a", "b").Order("created_at", "DESC").Limit(10).Offset(20).Build()),
			`SELECT id, title, posts.author_id FROM app.posts LEFT JOIN authors ON authors.id = posts.author_id WHERE (title = $1) OR (status IN ($2, $3)) ORDER BY created_at DESC LIMIT $4 OFFSET $5`},
		{first(Select("*").From("posts").Cursor("id", "p1", "backward").Build()),
			`SELECT * FROM posts WHERE (id < $1) ORDER BY id DESC`},
		{first(Update("posts").Set("title", "t").Set("updated_at", 1).Where("id = $1", 2).Returning("id").Build()),
			`UPDATE posts SET title = $1, updated_at = $2 WHERE (id = $3) RETURNING id`},
		{first(Delete("posts").Where("id = $1", 1).Build()), `DELETE FROM posts WHERE (id = $1)`},
		{first(Count("posts").Where("deleted_at IS NULL").Build()), `SELECT COUNT(*) FROM posts WHERE (deleted_at IS NULL)`},
	}
	for _, c := range cases {
		if c.sql != c.want {
			t.Errorf("\n got %s\nwant %s", c.sql, c.want)
		}
	}
}

func first(sql string, _ []any) string { return sql }
