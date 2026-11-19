package entity_test

import (
	"database/sql"
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/internal/testdb"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

func TestTypedLikePatternParity(t *testing.T) {
	testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, _ migrate.Dialect) {
		if _, err := db.Exec(`CREATE TABLE samples (id TEXT PRIMARY KEY, "order" TEXT)`); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO samples (id, "order") VALUES
			('a', 'Alpha'), ('b', 'a_b'), ('c', 'a%b'), ('d', 'aXb'),
			('e', 'Écho'), ('f', 'écho'), ('g', $1), ('h', NULL), ('i', 'a!b'),
			('j', 'ABCDEFGHIJKLMNOPQRSTUVWXYZ')`, `a\`); err != nil {
			t.Fatal(err)
		}
		col := entity.NewStringColumn("order")
		for _, tc := range []struct {
			name, pattern string
			want          []string
		}{
			{"ascii-case", "aL%", []string{"a"}},
			{"ascii-alphabet", "abcdefghijklmnopqrstuvwxyz", []string{"j"}},
			{"unicode-case", "É%", []string{"e"}},
			{"unicode-wildcard", "_cho", []string{"e", "f"}},
			{"underscore", `A\_b`, []string{"b"}},
			{"percent", `a\%b`, []string{"c"}},
			{"one-character", "a_b", []string{"b", "c", "d", "i"}},
			{"escaped-backslash", `a\\`, []string{"g"}},
			{"trailing-backslash", `a\`, []string{"g"}},
			{"ordinary-punctuation", "a!b", []string{"i"}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				var complement []string
				for _, id := range []string{"a", "b", "c", "d", "e", "f", "g", "i", "j"} {
					if !slices.Contains(tc.want, id) {
						complement = append(complement, id)
					}
				}
				for _, op := range []struct {
					name string
					cond entity.Condition
					want []string
				}{
					{"Like", col.Like(tc.pattern), tc.want},
					{"NotLike", col.NotLike(tc.pattern), complement},
				} {
					qb := query.Select("id").From("samples").Where("id != $1", "excluded").Order("id", "ASC")
					op.cond.Apply(qb)
					qb.Where("id != $1", "also-excluded")
					stmt, args := qb.Build()
					rows, err := db.QueryContext(t.Context(), stmt, args...)
					if err != nil {
						t.Errorf("%s(%q): %v", op.name, tc.pattern, err)
						continue
					}
					var got []string
					for rows.Next() {
						var id string
						if err := rows.Scan(&id); err != nil {
							rows.Close()
							t.Fatal(err)
						}
						got = append(got, id)
					}
					rows.Close()
					if err := rows.Err(); err != nil || !slices.Equal(got, op.want) {
						t.Errorf("%s(%q): %v, %v; want %v", op.name, tc.pattern, got, err, op.want)
					}
				}
			})
		}
	})
}
