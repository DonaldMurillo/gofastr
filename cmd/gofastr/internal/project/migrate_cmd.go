package project

import (
	"strings"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// CanonicalDBDriver maps sqlite(3) to sqlite3, postgres(ql) to postgres and mysql to mysql, in any letter case; any other value comes back trimmed with its case kept.
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
