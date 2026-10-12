package admin

import (
	"html"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/entity"
)

var auditRowIDRe = regexp.MustCompile(`<tr id="(a[0-9])"`)

// auditIDsShown lists the audit rows a page draws, in order.
func auditIDsShown(body string) []string {
	var out []string
	for _, m := range auditRowIDRe.FindAllStringSubmatch(body, -1) {
		out = append(out, m[1])
	}
	return out
}

// The audit log pages back through older rows, newest first, a page at
// a time; a page turn keeps the filter, and a page past the end shows
// the last one.
func TestAuditPagesOlderRows(t *testing.T) {
	x := setup(t, map[string]entity.EntityConfig{"posts": postsConfig()}, Config{Entities: []string{"posts"}, AuditListLimit: 2}, nil)
	now := time.Now().UTC()
	for i, id := range []string{"a1", "a2", "a3", "a4", "a5"} {
		x.seedAudit(id, "", "posts", "create", "r-"+id, "u1", now.Add(time.Duration(i)*time.Second))
	}
	x.seedAudit("b1", "", "posts", "create", "r-other", "u2", now.Add(time.Minute))
	for _, c := range []struct {
		query string
		want  []string
	}{
		{"actor=u1", []string{"a5", "a4"}},
		{"actor=u1&p=2", []string{"a3", "a2"}},
		{"actor=u1&p=3", []string{"a1"}},
		{"actor=u1&p=99", []string{"a1"}},
	} {
		body := get(x.as(theAdmin), "/admin/audit?"+c.query).Body.String()
		if got := auditIDsShown(body); !slices.Equal(got, c.want) {
			t.Errorf("%s shows %v, want %v", c.query, got, c.want)
		}
	}
	body := get(x.as(theAdmin), "/admin/audit?actor=u1").Body.String()
	next := regexp.MustCompile(`<a[^>]*href="([^"]*)"[^>]*>3</a>`).FindStringSubmatch(body)
	if next == nil {
		t.Fatalf("the first page has no link to page 3:\n%s", body)
	}
	if href := html.UnescapeString(next[1]); !strings.Contains(href, "actor=u1") || !strings.Contains(href, "p=3") {
		t.Errorf("page 3's link %q drops the filter or the page", href)
	}
}
