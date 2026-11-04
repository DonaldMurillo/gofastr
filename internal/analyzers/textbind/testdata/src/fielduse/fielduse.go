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
