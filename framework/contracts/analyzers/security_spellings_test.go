package analyzers_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
	_ "github.com/DonaldMurillo/gofastr/framework/contracts/analyzers"
)

// ruleLines runs every analyzer over a one-file module and returns the
// lines ruleID reports in main.go.
func ruleLines(t *testing.T, ruleID, src string) []int {
	t.Helper()
	dir := t.TempDir()
	for name, body := range map[string]string{"go.mod": "module example.com/app\n\ngo 1.26\n", "main.go": src} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	pass, err := contracts.NewPass(dir, contracts.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	report, err := contracts.Run(pass, contracts.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var lines []int
	for _, d := range report.Diagnostics {
		if d.RuleID == ruleID && d.File == "main.go" {
			lines = append(lines, d.Line)
		}
	}
	slices.Sort(lines)
	return lines
}

// A security rule's documented shape fires however the code spells it:
// an import alias, a Sprintf in place of `+`, a build split over lines.
// GOFASTR1404 keyed on the identifier `http`, so `nethttp.Cookie{...}`
// passed; GOFASTR1403 was a one-line regex for `render.HTML(... + ...)`,
// so render.HTML(fmt.Sprintf("<p>%s</p>", name)) and a concatenation
// broken across lines put the same unescaped value into markup unseen.
func TestSecurityRulesFireOnEverySpelling(t *testing.T) {
	cases := []struct {
		name, rule, src string
		want            []int
	}{
		{"cookie under an import alias", contracts.RuleInsecureCookie, `package main

import nethttp "net/http"

func f(w nethttp.ResponseWriter, v string) {
	nethttp.SetCookie(w, &nethttp.Cookie{Name: "sid", Value: v})
}
`, []int{6}},
		{"a local package named http is not net/http", contracts.RuleInsecureCookie, `package main

import http "example.com/app/myhttp"

func f(v string) any {
	return http.Cookie{Name: "sid", Value: v}
}
`, nil},
		{"render.HTML over Sprintf, Sprint, a split and a nested call", contracts.RuleHTMLConcat, `package main

import (
	"fmt"

	r "github.com/DonaldMurillo/gofastr/core/render"
)

func f(name string, esc func(string) string) []r.HTML {
	return []r.HTML{
		r.HTML(fmt.Sprintf("<p>%s</p>", name)),
		r.HTML(fmt.Sprint("<p>", name, "</p>")),
		r.HTML("<p>" +
			name + "</p>"),
		r.HTML(esc(name) + "</p>"),
	}
}
`, []int{11, 12, 13, 15}},
		{"render.HTML over a non-literal format, strings.Join and a built local", contracts.RuleHTMLConcat, `package main

import (
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func f(tmpl, name string, parts []string) []render.HTML {
	s := "<p>" + name + "</p>"
	var t = fmt.Sprintf("<b>%s</b>", name)
	return []render.HTML{
		render.HTML(fmt.Sprintf(tmpl)),
		render.HTML(strings.Join(parts, "")),
		render.HTML(s),
		render.HTML(t),
	}
}
`, []int{14, 15, 16, 17}},
		{"render.HTML over a constant local, Join of literals and a plain variable stays quiet", contracts.RuleHTMLConcat, `package main

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func f(trusted string) []render.HTML {
	s := "<p>" + "</p>"
	return []render.HTML{
		render.HTML(s),
		render.HTML(strings.Join([]string{"<p>", "</p>"}, "")),
		render.HTML(trusted),
	}
}
`, nil},
		{"render.HTML over constants stays quiet", contracts.RuleHTMLConcat, `package main

import (
	"fmt"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func f() []render.HTML {
	return []render.HTML{
		render.HTML(fmt.Sprintf("<p>%d</p>", 3)),
		render.HTML("<p>" +
			"</p>"),
	}
}
`, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ruleLines(t, tc.rule, tc.src); !slices.Equal(got, tc.want) {
				t.Errorf("%s reported lines %v, want %v", tc.rule, got, tc.want)
			}
		})
	}
}
