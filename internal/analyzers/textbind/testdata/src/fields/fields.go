// Package fields: request text stored in a struct field and bound later
// through the field (R8-AA-01: battery/admin's grantChange.role).
package fields

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"unicode/utf8"
)

var db *sql.DB

type change struct {
	role  string // want role:`request text`
	perm  string
	grant bool
}

// A literal keyed write from the form, bound in another function.
func handleKeyed(w http.ResponseWriter, r *http.Request) {
	apply([]change{{role: r.FormValue("role"), perm: "read"}})
}

func apply(cs []change) {
	for _, c := range cs {
		db.ExecContext(context.Background(), "INSERT INTO grants (role, perm) VALUES ($1, $2)", c.role, c.perm) // want `request text reaches a SQL bind`
	}
}

type target struct {
	name string // want name:`request text`
	note string
}

// An assignment to the field.
func handleAssign(w http.ResponseWriter, r *http.Request) {
	var t target
	t.name = strings.TrimSpace(r.PostFormValue("name"))
	t.note = "fixed"
	save(t)
}

func save(t target) {
	db.Exec("UPDATE t SET name = $1, note = $2", t.name, t.note) // want `request text reaches a SQL bind`
}

type entry struct {
	key string // want key:`request text`
}

// A constructor stores its parameter; one caller hands it request text.
func newEntry(key string) entry { return entry{key: key} }

func handleCtor(w http.ResponseWriter, r *http.Request) {
	store(newEntry(r.PathValue("key")))
}

func store(e entry) {
	db.Exec("INSERT INTO e (k) VALUES ($1)", e.key) // want `request text reaches a SQL bind`
}

type pair struct {
	a string // want a:`request text`
	b string
}

// A positional literal.
func handlePositional(w http.ResponseWriter, r *http.Request) {
	p := pair{r.Header.Get("X-A"), "b"}
	db.Exec("INSERT INTO p (a, b) VALUES ($1, $2)", p.a, p.b) // want `request text reaches a SQL bind`
}

// Exported is read by fielduse, another package.
type Exported struct {
	Label string // want Label:`request text`
}

func NewExported(r *http.Request) Exported {
	return Exported{Label: r.FormValue("label")}
}

// --- quiet ---------------------------------------------------------

type vetted struct {
	role string
}

// Checked before it is stored: the field never holds unvetted bytes.
func handleChecked(w http.ResponseWriter, r *http.Request) {
	role := r.FormValue("role")
	if !utf8.ValidString(role) || strings.IndexByte(role, 0) >= 0 {
		http.Error(w, "bad role", http.StatusBadRequest)
		return
	}
	v := vetted{role: role}
	db.Exec("INSERT INTO v (role) VALUES ($1)", v.role)
}

type config struct {
	table string
}

// Only constants and configuration reach the field.
func fromConfig() {
	c := config{table: "grants"}
	c.table = "audit"
	db.Exec("INSERT INTO c (t) VALUES ($1)", c.table)
}

type tag struct {
	val string
}

func newTag(v string) tag { return tag{val: v} }

// The constructor's only callers pass constants.
func constCtor() {
	db.Exec("INSERT INTO tags (v) VALUES ($1)", newTag("x").val)
}

type helperVetted struct {
	name string
}

// Checked through a helper declared below the caller: the field marks
// only once every check is known.
func handleHelperChecked(w http.ResponseWriter, r *http.Request) {
	name := r.FormValue("name")
	if !okText(name) {
		http.Error(w, "bad name", http.StatusBadRequest)
		return
	}
	v := helperVetted{name: name}
	db.Exec("INSERT INTO h (name) VALUES ($1)", v.name)
}

func okText(s string) bool { // want okText:`textbind\(sink=0 return=0 check=1 source=false\)`
	return utf8.ValidString(s) && strings.IndexByte(s, 0) < 0
}
