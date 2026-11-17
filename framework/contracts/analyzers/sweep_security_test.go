package analyzers_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

func TestSweepSecuritySQLDynamicIdentifierStaysQuiet(t *testing.T) {
	src := `package main

import "fmt"

type request struct{ Table string }

func f(table string, req request) []string {
	return []string{
		fmt.Sprintf("SELECT * FROM %s WHERE id = $1", table),
		fmt.Sprintf("SELECT * FROM %s WHERE id = $1", req.Table),
	}
}
`
	if got := ruleLines(t, contracts.RuleSQLStringConcat, src); !slices.Equal(got, []int{10}) {
		t.Errorf("GOFASTR1401 lines %v, want [10]: an unquoted identifier fires only with a request-derived name", got)
	}
}

func TestSweepSecuritySQLInsertOrIgnoreIsAStatement(t *testing.T) {
	src := `package main

import "database/sql"

func f(db *sql.DB, name string) {
	_, err := db.Exec("INSERT OR IGNORE INTO tags (name) VALUES ('" + name + "')")
	_ = err
}
`
	if got := ruleLines(t, contracts.RuleSQLStringConcat, src); !slices.Equal(got, []int{6}) {
		t.Errorf("GOFASTR1401 lines %v, want [6]", got)
	}
}

func TestSweepSecurityCSRFCountsEachForm(t *testing.T) {
	src := "package main\n\nvar CSRFInputFromCtx = func(any) string { return \"\" }\n\n" +
		"func pages(ctx any) []string {\n" +
		"\treturn []string{\n" +
		"\t\t`<form method=\"POST\" action=\"/a\">` + CSRFInputFromCtx(ctx) + `</form>`,\n" +
		"\t\t`<form method=\"POST\" action=\"/b\"></form>`,\n" +
		"\t}\n}\n"
	if got := ruleLines(t, contracts.RuleFormWithoutCSRF, src); len(got) != 1 {
		t.Errorf("GOFASTR1402 lines %v, want one finding: one wired form does not cover a second", got)
	}
	exempt := strings.Replace(src, "\treturn []string{\n", "\t// csrf-exempt: /b is a public search form\n\treturn []string{\n", 1)
	if got := ruleLines(t, contracts.RuleFormWithoutCSRF, exempt); got != nil {
		t.Errorf("GOFASTR1402 lines %v, want none: a csrf-exempt comment counts as a wiring", got)
	}
}

func TestSweepSecurityCookieExplicitFalseIsNotProtection(t *testing.T) {
	src := `package main

import "net/http"

func f(w http.ResponseWriter, v string) {
	http.SetCookie(w, &http.Cookie{Name: "sid", Value: v, HttpOnly: false, Secure: true, SameSite: http.SameSiteLaxMode})
}
`
	if got := ruleLines(t, contracts.RuleInsecureCookie, src); !slices.Equal(got, []int{6}) {
		t.Errorf("GOFASTR1404 lines %v, want [6]: HttpOnly: false protects nothing", got)
	}
}

func TestSweepSecurityCookieFixAnchorsOnTheLiteralTail(t *testing.T) {
	ds := fixture(t, map[string]string{"h.go": `package main

import "net/http"

func f(w http.ResponseWriter, v string) {
	http.SetCookie(w, &http.Cookie{
		Name:  "sid",
		Value: v,
	})
}
`})
	d := assertHas(t, ds, contracts.RuleInsecureCookie)
	if d.Fix == nil || len(d.Fix.Edits) != 1 {
		t.Fatalf("want one fix edit, got %+v", d.Fix)
	}
	if old := d.Fix.Edits[0].Old; old != ",\n\t}" {
		t.Errorf("fix Old = %q: want the run from the last element through the brace", old)
	}
}

func TestSweepSecurityForwardedProtoUnreflectedReadStaysQuiet(t *testing.T) {
	src := `package main

import (
	"log"
	"net/http"
)

func f(w http.ResponseWriter, r *http.Request, out *http.Request) {
	if r.Header.Get("X-Forwarded-Proto") != "" {
		log.Print("behind a proxy")
	}
	out.Header.Set("X-Forwarded-Proto", "https")
}
`
	if got := ruleLines(t, contracts.RuleForwardedProtoEnum, src); got != nil {
		t.Errorf("GOFASTR1406 lines %v, want none: a read that never reaches output is not a reflection", got)
	}
}

func TestSweepSecurityZeroArgReadNeedsCoreStream(t *testing.T) {
	src := `package main

import "encoding/json"

type journal struct{}

func (j *journal) Read() ([]byte, error) { return nil, nil }

func f(j *journal) {
	raw, _ := j.Read()
	var v any
	_ = json.Unmarshal(raw, &v)
}
`
	if got := ruleLines(t, contracts.RuleRawJSONBodyDecode, src); got != nil {
		t.Errorf("GOFASTR1407 lines %v, want none: a zero-argument Read outside core/stream is not a frame", got)
	}
}

func TestSweepSecurityMethodResultReadsOnlySourceFields(t *testing.T) {
	src := `package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os/exec"
)

type client struct{ stdout io.ReadCloser }

func start(cmd *exec.Cmd) *client {
	pipe, _ := cmd.StdoutPipe()
	return &client{stdout: pipe}
}

type rpcError struct{ Message []byte }

func (e *rpcError) Text() []byte { return nil }

func handle(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	e := &rpcError{}
	e.Message = raw
}

func render(e *rpcError) {
	b := e.Text()
	var v any
	_ = json.Unmarshal(b, &v)
}
`
	if got := ruleLines(t, contracts.RuleRawJSONBodyDecode, src); got != nil {
		t.Errorf("GOFASTR1407 lines %v, want none: a field fed a decoded value is not a source its methods return", got)
	}
}

func TestSweepSecurityURLAttrSlotsAndVerbIndex(t *testing.T) {
	src := `package main

import (
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func f(b *strings.Builder, u string) string {
	b.WriteString("<div data-src=\"" + render.Escape(u) + "\">")
	return fmt.Sprintf("<p style=\"width:100%%\"><a href=\"%s\">", render.Escape(u))
}
`
	if got := ruleLines(t, contracts.RuleURLAttrEscape, src); !slices.Equal(got, []int{12}) {
		t.Errorf("GOFASTR1412 lines %v, want [12]: data-src is no URL slot, and %%%% is not a verb", got)
	}
}

func TestSweepSecurityURLAttrGlueAndReassignedRoot(t *testing.T) {
	src := `package main

import (
	"html"
	"strings"
)

func f(b *strings.Builder, raw string) {
	b.WriteString("<a href=\"" + "/" + html.EscapeString(raw) + "\">")
	u := "/home"
	u = raw
	b.WriteString("<a href=\"" + html.EscapeString(u) + "\">")
}
`
	if got := ruleLines(t, contracts.RuleURLAttrEscape, src); !slices.Equal(got, []int{9, 12}) {
		t.Errorf("GOFASTR1412 lines %v, want [9 12]: glue before the escape still fills the slot, and a reassigned root is no longer rooted", got)
	}
}
