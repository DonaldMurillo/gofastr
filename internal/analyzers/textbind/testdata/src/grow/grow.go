// Package grow declares a field's reader before the constructor that
// fills it: the field is marked in a later round, and the fixed point
// runs again because a field grew even when no summary changed.
package grow

import (
	"database/sql"
	"net/http"
)

var db *sql.DB

type late struct {
	s string // want s:`request text`
}

func readLate(l late) string { return l.s } // want readLate:`source=true`

func bindLate(l late) {
	db.Exec("INSERT INTO l (s) VALUES ($1)", readLate(l)) // want `request text reaches a SQL bind`
}

func handle(w http.ResponseWriter, r *http.Request) {
	bindLate(mk(r.FormValue("s")))
}

func mk(s string) late { return late{s: s} }
