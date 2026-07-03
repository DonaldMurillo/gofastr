package docs

import (
	"io/fs"
	"strings"
	"testing"
)

// A cheap guard against the class of doc drift where a code sample cites an
// API that doesn't exist. It doesn't compile the samples (too many are
// illustrative fragments); it pins the SPECIFIC wrong forms that were found
// and fixed in the 2026-07 audit so they can't silently return. Each entry
// is a substring that must never appear in an embedded doc, paired with the
// correct form for the failure message.
func TestDocsAvoidKnownWrongAPIs(t *testing.T) {
	banned := []struct{ bad, fix string }{
		{"app.Router.", "App.Router is a method — use app.Router().Method(...)"},
		{"Router.With(", "core/router has no With(); wrap the handler: RequirePermission(perm)(h)"},
		{".HasRole(", "no HasRole method — use slices.Contains(u.GetRoles(), role)"},
		{"Registry.Names()", "entity Registry has no Names(); range over Registry.All()"},
		{`sched.Every("`, "cron.Scheduler has no Every(string, fn); use Register(CronJob{Spec: ...})"},
		{"migrate diff", "`migrate diff` was removed — use `migrate generate <name>`"},
	}

	entries, err := fs.ReadDir(contentFS, "content")
	if err != nil {
		t.Fatalf("read content dir: %v", err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		body, err := fs.ReadFile(contentFS, "content/"+e.Name())
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		text := string(body)
		for _, b := range banned {
			if strings.Contains(text, b.bad) {
				t.Errorf("%s contains the known-wrong doc API %q — %s", e.Name(), b.bad, b.fix)
			}
		}
	}
}
