package project

import (
	"strings"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// CanonicalDBDriver maps the database spellings the CLI accepts to the
// database/sql driver name: the docs-canonical sqlite and postgres
// plus the aliases sqlite3 and postgresql. It is the one alias table
// `gofastr init --db=` and `gofastr migrate --db=/--driver=` share. Any
// other value comes back trimmed, unchanged, for the caller to refuse
// (init) or check against the registered drivers (migrate).
func CanonicalDBDriver(name string) string {
	name = strings.TrimSpace(name)
	switch strings.ToLower(name) {
	case "sqlite", "sqlite3":
		return "sqlite3"
	case "postgres", "postgresql":
		return "postgres"
	case "mysql":
		return "mysql"
	}
	return name
}
