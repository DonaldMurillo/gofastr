//go:build red

package admin

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// CONTRACT-QUESTION red: the repo's architecture contract (CLAUDE.md TL;DR: "The interactive
// layer is stateless: state lives in the DB or the client signal store, never in server RAM.
// Sessions are signed tokens; any replica serves any request") says a redirect-mediated
// handoff must resolve on any replica. The admin battery's form-flash is process-local RAM
// keyed by an opaque token in the redirect URL, so a 303 from replica A renders as a BLANK
// form on replica B: the validation error and the operator's submitted values silently
// vanish. The maintainer must decide: signed-cookie flash (replica-safe, matches the session
// design) or an explicit documented exception for the 2-minute flash. Asserted the secure
// reading; fails today.
// Family: F8 multi-replica statelessness
// Surfaces: battery/admin/entity_admin.go::flashStore (process-local map, no shared-store option),
//           battery/admin/entity_admin.go::entitySave (303 with ?e=<token>),
//           battery/admin/entity_screens.go::entityFormScreen.Load (pops the flash from RAM).
// Finding: a validation-failed save redirected by app A renders on app B without the flash:
// the submitted values and field errors are dropped, silently. Severity: low — correctness
// under multi-replica, no data exposure; but it contradicts the documented statelessness
// contract unless the maintainer carves out an exception.
// Fix direction: carry the flash in a signed short-TTL cookie (the sessions pattern) or stash
// it in the DB keyed by the token.

func TestFlashResolvesOnSecondReplica(t *testing.T) {
	db := newDB(t)
	// Two apps over one database: the multi-replica shape. Each mounts its own
	// admin battery, so each has its own in-process flash store.
	appA := newHostedApp(t, db, map[string]entity.EntityConfig{"posts": postsConfig()})
	appB := newHostedApp(t, db, map[string]entity.EntityConfig{"posts": postsConfig()})
	replicaA := mountEntityAdmin(t, appA, Config{Entities: []string{"posts"}}, testUser{"u1"})
	replicaB := mountEntityAdmin(t, appB, Config{Entities: []string{"posts"}}, testUser{"u1"})

	// A save that fails validation on replica A redirects with a flash token.
	rr := postForm(replicaA, "/admin/e/posts/_create", url.Values{"title": {""}, "body": {"kept-note"}})
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("create: %d body=%s", rr.Code, rr.Body.String())
	}
	loc := rr.Header().Get("Location")
	if !strings.Contains(loc, "e=") {
		t.Fatalf("redirect carries no flash token: %q", loc)
	}

	// The browser follows the redirect; with round-robin DNS it lands on replica B.
	followed := get(replicaB, loc)
	if followed.Code != http.StatusOK {
		t.Fatalf("follow redirect on replica B: %d", followed.Code)
	}
	if !strings.Contains(followed.Body.String(), "kept-note") {
		t.Fatalf("CONTRACT-QUESTION [admin-flash]: replica B rendered the redirected form without the flash payload (submitted value %q lost) — per the repo statelessness contract any replica must serve the redirect; today the flash lives only in replica A's RAM", "kept-note")
	}
}
