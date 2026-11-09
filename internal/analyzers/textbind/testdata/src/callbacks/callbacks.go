// Package callbacks: request text arriving through a host callback, a
// func-typed struct field the host installs (d6167d1a's
// IdempotencyConfig.Principal, whose result reached
// SQLIdempotencyStore.Begin's key bind unchecked).
package callbacks

import (
	"database/sql"
	"net/http"
	"strings"
	"unicode/utf8"
)

var db *sql.DB

type Config struct {
	// A host callback: this package never fills it, and the host reads
	// the request through it.
	Principal func(r *http.Request) string
	// Filled in-package below. The fill union is field-wide, like the
	// data-field arm: one unchecked fill taints every call through the
	// field.
	Tenant func(r *http.Request) string
	// Filled only by checking callbacks.
	Checked func(r *http.Request) string
	// Filled only by a callback that returns no request text.
	Fixed func(r *http.Request) string
	// Not request-shaped: no *http.Request parameter.
	Label func() string
}

// No fill anywhere in the package: the call is request text, the shape
// d6167d1a fixed at the store.
func noFill(c Config, w http.ResponseWriter, r *http.Request) {
	p := ""
	if c.Principal != nil {
		p = c.Principal(r)
	}
	db.Exec("INSERT INTO s (p) VALUES ($1)", p) // want `request text reaches a SQL bind`
}

// A same-package fill whose literal reads the request.
func filled(w http.ResponseWriter, r *http.Request) {
	c := Config{}
	c.Tenant = func(r *http.Request) string { return r.Header.Get("X-Tenant") }
	t := c.Tenant(r)
	db.Exec("INSERT INTO s (t) VALUES ($1)", t) // want `request text reaches a SQL bind`
}

// A same-package fill by a named function that reads the request.
func tenantOf(r *http.Request) string { return r.Header.Get("X-Tenant") } // want tenantOf:`textbind\(sink=0 return=0 check=0 source=true retfields=\[\]\)`

func filledNamed(w http.ResponseWriter, r *http.Request) {
	c := Config{Tenant: tenantOf}
	t := c.Tenant(r)
	db.Exec("INSERT INTO s (t) VALUES ($1)", t) // want `request text reaches a SQL bind`
}

// A fill whose literal refuses unstorable text vets the call.
func checkedFill(w http.ResponseWriter, r *http.Request) {
	c := Config{}
	c.Checked = func(r *http.Request) string {
		t := r.Header.Get("X-Tenant")
		if !utf8.ValidString(t) || strings.IndexByte(t, 0) >= 0 {
			return ""
		}
		return t
	}
	t := c.Checked(r)
	db.Exec("INSERT INTO s (t) VALUES ($1)", t)
}

// A fill that returns no request text vets the call.
func fixedTenant(r *http.Request) string { return "main" }

func constFill(w http.ResponseWriter, r *http.Request) {
	c := Config{Fixed: fixedTenant}
	t := c.Fixed(r)
	db.Exec("INSERT INTO s (t) VALUES ($1)", t)
}

// A callback that never sees the request is not request text.
func plain(c Config) {
	l := c.Label()
	db.Exec("INSERT INTO s (l) VALUES ($1)", l)
}
