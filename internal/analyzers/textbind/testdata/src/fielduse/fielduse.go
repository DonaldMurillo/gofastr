// Package fielduse reads a field another package fills with request text.
package fielduse

import (
	"database/sql"
	"net/http"

	"fields"
)

var db *sql.DB

func handle(w http.ResponseWriter, r *http.Request) {
	e := fields.NewExported(r)
	db.Exec("INSERT INTO l (label) VALUES ($1)", e.Label) // want `request text reaches a SQL bind`
}

// A field another package declares, filled with request text here and
// read back here.
func foreignField(w http.ResponseWriter, r *http.Request) {
	p := fields.Plain{Key: r.FormValue("k")}
	db.Exec("INSERT INTO p (k) VALUES ($1)", p.Key) // want `request text reaches a SQL bind`
}

// The 1de1c216/155cafcd shape: the field is filled here with request
// text and read back through the declaring package's method.
func methodRead(w http.ResponseWriter, r *http.Request) {
	p := fields.Plain{Key: r.FormValue("k")}
	db.Exec("INSERT INTO p (k) VALUES ($1)", p.Bound()) // want `request text reaches a SQL bind`
}
