package admin

import (
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// url.ParseQuery drops a pair it cannot decode, so the screen would list
// wider than the reader asked. Each admin screen with a narrowing query
// warns instead, the way the entityui lists do.
func TestUndecodablePairWarns(t *testing.T) {
	const warning = "Filter not applied"
	x := setup(t, map[string]entity.EntityConfig{"posts": postsConfig(), "notes": notesConfig()}, Config{Entities: []string{"posts", "notes"}}, nil)
	seedAuditDays(x, time.Now().UTC())
	for _, p := range []string{
		"/admin/audit?entity=posts%ZZ",
		"/admin/audit?actor=u1%ZZ",
		"/admin/audit?op=create%ZZ",
		"/admin/audit?from=2026-01-01%ZZ",
		"/admin/audit?ent%ZZity=posts",
	} {
		body := get(x.as(theAdmin), p).Body.String()
		if !strings.Contains(body, warning) {
			t.Errorf("%s drew no warning", p)
		}
	}
	if body := get(x.as(theAdmin), "/admin/audit?entity=posts&limit=5%ZZ").Body.String(); strings.Contains(body, warning) {
		t.Error("an undecodable limit, which narrows nothing, warned")
	}

	q := &fakeQueue{}
	qx := queueEnv(t, q)
	if body := get(qx.as(theAdmin), "/admin/queue?status=failed%ZZ").Body.String(); !strings.Contains(body, warning) {
		t.Error("an undecodable status on the jobs page drew no warning")
	}
	if body := get(qx.as(theAdmin), "/admin/queue?status=failed").Body.String(); strings.Contains(body, warning) {
		t.Error("a decodable status warned")
	}

	px := paletteEnv(t, Config{})
	if body := get(px.as(theAdmin), "/admin/search?q=launch%ZZ").Body.String(); !strings.Contains(body, warning) {
		t.Error("an undecodable search query drew no warning")
	}
}
