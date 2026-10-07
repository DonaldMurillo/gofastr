package admin

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// seedAuditDays seeds one row per day so the date range has distinct
// anchors: day 1 is earliest.
func seedAuditDays(x *env, now time.Time) {
	x.seedAudit("d1", "", "posts", "create", "r-day1", "u1", now.AddDate(0, 0, -2))
	x.seedAudit("d2", "", "posts", "delete", "r-day2", "u2", now.AddDate(0, 0, -1))
	x.seedAudit("d3", "", "notes", "create", "r-day3", "u1", now)
}

func auditRowsShown(body string) int {
	// One Record cell per data row; linked records also appear in hrefs,
	// so counting the record id itself would double-count.
	return strings.Count(body, `data-label="Record"`)
}

func TestAuditFilterByActor(t *testing.T) {
	x := setup(t, map[string]entity.EntityConfig{"posts": postsConfig(), "notes": notesConfig()}, Config{Entities: []string{"posts", "notes"}}, nil)
	seedAuditDays(x, time.Now().UTC())
	body := get(x.as(theAdmin), "/admin/audit?actor=u1").Body.String()
	if auditRowsShown(body) != 2 {
		t.Fatalf("actor=u1 showed %d rows, want the two u1 rows:\n%s", auditRowsShown(body), body)
	}
	if strings.Contains(body, "r-day2") {
		t.Error("actor=u1 showed another actor's row")
	}
}

func TestAuditFilterByEntityAndOp(t *testing.T) {
	x := setup(t, map[string]entity.EntityConfig{"posts": postsConfig(), "notes": notesConfig()}, Config{Entities: []string{"posts", "notes"}}, nil)
	seedAuditDays(x, time.Now().UTC())
	body := get(x.as(theAdmin), "/admin/audit?entity=posts&op=delete").Body.String()
	if auditRowsShown(body) != 1 || !strings.Contains(body, "r-day2") {
		t.Fatalf("entity=posts&op=delete showed the wrong rows:\n%s", body)
	}
}

func TestAuditFilterByDateRange(t *testing.T) {
	x := setup(t, map[string]entity.EntityConfig{"posts": postsConfig(), "notes": notesConfig()}, Config{Entities: []string{"posts", "notes"}}, nil)
	now := time.Now().UTC()
	seedAuditDays(x, now)
	day2 := now.AddDate(0, 0, -1).Format("2006-01-02")
	// to is inclusive: from=to=day2 narrows to the middle day only.
	body := get(x.as(theAdmin), "/admin/audit?from="+day2+"&to="+day2).Body.String()
	if auditRowsShown(body) != 1 || !strings.Contains(body, "r-day2") {
		t.Fatalf("from=to=%s showed the wrong rows:\n%s", day2, body)
	}
	// from=day2 leaves day2 and day3.
	body = get(x.as(theAdmin), "/admin/audit?from="+day2).Body.String()
	if auditRowsShown(body) != 2 || strings.Contains(body, "r-day1") {
		t.Fatalf("from=%s showed the wrong rows:\n%s", day2, body)
	}
}

func TestAuditFiltersCombineWithAnd(t *testing.T) {
	x := setup(t, map[string]entity.EntityConfig{"posts": postsConfig(), "notes": notesConfig()}, Config{Entities: []string{"posts", "notes"}}, nil)
	seedAuditDays(x, time.Now().UTC())
	// entity=notes alone shows r-day3; adding actor=u2 narrows to nothing.
	body := get(x.as(theAdmin), "/admin/audit?entity=notes&actor=u2").Body.String()
	if auditRowsShown(body) != 0 {
		t.Fatalf("entity=notes&actor=u2 showed %d rows, want none:\n%s", auditRowsShown(body), body)
	}
}

func TestAuditFilterIgnoresBadValues(t *testing.T) {
	x := setup(t, map[string]entity.EntityConfig{"posts": postsConfig(), "notes": notesConfig()}, Config{Entities: []string{"posts", "notes"}}, nil)
	seedAuditDays(x, time.Now().UTC())
	q := url.Values{}
	q.Set("entity", "nope")
	q.Set("op", "rm -rf")
	q.Set("from", "not-a-date")
	q.Set("to", "31-12-2026")
	q.Set("actor", strings.Repeat("x", 201))
	rr := get(x.as(theAdmin), "/admin/audit?"+q.Encode())
	if rr.Code != 200 {
		t.Fatalf("bad values answered %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	if auditRowsShown(body) != 3 {
		t.Fatalf("invalid filters narrowed the list: %d rows shown, want all 3", auditRowsShown(body))
	}
	for _, param := range []string{"entity", "op", "from", "to", "actor"} {
		if !strings.Contains(body, param) {
			t.Errorf("no warning names the ignored %q param", param)
		}
	}
	if !strings.Contains(body, "fui-callout") {
		t.Error("an ignored filter draws no warning callout")
	}
	// Control bytes in actor are ignored too.
	rr = get(x.as(theAdmin), "/admin/audit?actor="+url.QueryEscape("a\x00b"))
	if rr.Code != 200 || auditRowsShown(rr.Body.String()) != 3 {
		t.Fatalf("a control-byte actor answered %d with %d rows, want 200 and all rows", rr.Code, auditRowsShown(rr.Body.String()))
	}
}

func TestAuditFilterInjectionActorMatchesNothing(t *testing.T) {
	x := setup(t, map[string]entity.EntityConfig{"posts": postsConfig()}, Config{Entities: []string{"posts"}}, nil)
	seedAuditDays(x, time.Now().UTC())
	rr := get(x.as(theAdmin), "/admin/audit?actor="+url.QueryEscape("' OR 1=1 --"))
	if rr.Code != 200 {
		t.Fatalf("injection-shaped actor answered %d, want 200", rr.Code)
	}
	if n := auditRowsShown(rr.Body.String()); n != 0 {
		t.Fatalf("SECURITY: injection-shaped actor matched %d rows, want none", n)
	}
}

func TestAuditFilterFormDraws(t *testing.T) {
	x := setup(t, map[string]entity.EntityConfig{"posts": postsConfig(), "notes": notesConfig()}, Config{Entities: []string{"posts", "notes"}}, nil)
	body := get(x.as(theAdmin), "/admin/audit").Body.String()
	for _, want := range []string{
		`action="/admin/audit"`,
		`method="GET"`,
		`name="actor"`,
		`name="entity"`,
		`name="op"`,
		`name="from"`,
		`name="to"`,
		`type="date"`,
		`href="/admin/audit"`, // the clear link drops every param
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the filter form lacks %s:\n%s", want, body)
		}
	}
	// The entity select offers the exposed entities plus "any".
	if !strings.Contains(body, `value="posts">Posts`) || !strings.Contains(body, `value="notes">Notes`) {
		t.Error("the entity select does not offer the exposed entities")
	}
	// The current values round-trip into the form.
	body = get(x.as(theAdmin), "/admin/audit?entity=posts&op=delete&actor=u1").Body.String()
	if !strings.Contains(body, `selected="" value="posts"`) || !strings.Contains(body, `selected="" value="delete"`) {
		t.Error("the form does not echo the active filter")
	}
	if !strings.Contains(body, `value="u1"`) {
		t.Error("the form does not echo the actor")
	}
}

func TestAuditFilterStillCapsRows(t *testing.T) {
	x := setup(t, map[string]entity.EntityConfig{"posts": postsConfig()}, Config{Entities: []string{"posts"}, AuditListLimit: 2}, nil)
	now := time.Now().UTC()
	for i, id := range []string{"c1", "c2", "c3"} {
		x.seedAudit(id, "", "posts", "create", "r-cap"+id, "u1", now.Add(time.Duration(i)*time.Second))
	}
	body := get(x.as(theAdmin), "/admin/audit?actor=u1").Body.String()
	if n := auditRowsShown(body); n != 2 {
		t.Fatalf("the capped page showed %d rows, want the 2-row cap", n)
	}
}
