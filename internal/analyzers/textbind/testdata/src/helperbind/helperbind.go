// Package helperbind binds a parameter through a same-package helper
// before checking it. The check runs too late to vet the bind, so it
// earns no credit, and a caller binding the value again is reported.
package helperbind

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"
)

var db *sql.DB

var errBad = errors.New("bad")

func bindHelper(v string) { // want bindHelper:`textbind\(sink=1 return=0 check=0 source=false\)`
	db.ExecContext(context.Background(), "INSERT INTO t (v) VALUES ($1)", v)
}

func bindThenCheck(v string) error { // want bindThenCheck:`textbind\(sink=1 return=0 check=0 source=false\)`
	bindHelper(v)
	if !utf8.ValidString(v) || strings.IndexByte(v, 0) >= 0 {
		return errBad
	}
	return nil
}

func handle(w http.ResponseWriter, r *http.Request) {
	v := r.FormValue("v")
	if err := bindThenCheck(v); err != nil { // want `request text reaches a SQL bind`
		return
	}
	db.ExecContext(r.Context(), "INSERT INTO t2 (v) VALUES ($1)", v) // want `request text reaches a SQL bind`
}
